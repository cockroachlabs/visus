// Copyright 2022 Cockroach Labs Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package server defines the sub command to run visus in server mode.
package server

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cockroachdb/field-eng-powertools/stopper"
	"github.com/cockroachlabs/visus/internal/collector"
	"github.com/cockroachlabs/visus/internal/database"
	"github.com/cockroachlabs/visus/internal/http"
	"github.com/cockroachlabs/visus/internal/scanner"
	"github.com/cockroachlabs/visus/internal/server"
	"github.com/cockroachlabs/visus/internal/store"
	"github.com/go-co-op/gocron"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// Command runs the server.
func Command() *cobra.Command {
	cfg := &server.Config{}
	c := &cobra.Command{
		Use:  "start",
		Args: cobra.NoArgs,
		Example: `
./visus start --bindAddr "127.0.0.1:15432" `,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := stopper.WithContext(cmd.Context())

			if (cfg.BindCert == "" || cfg.BindKey == "") && !cfg.Insecure {
				return errors.New("--insecure must be specfied if certificates and private key are missing")
			}
			if cfg.URL == "" {
				return errors.New("--url must be specified")
			}
			// Set up database connections
			adminConn, err := database.New(ctx, cfg.URL)
			if err != nil {
				return err
			}
			store := store.New(adminConn)
			roConn, err := database.ReadOnly(ctx, cfg.URL, cfg.AllowUnsafeInternals)
			if err != nil {
				return err
			}
			// Set up Prometheus registry.
			registry := prometheus.NewRegistry()
			if err := server.RegisterMetrics(registry); err != nil {
				return err
			}

			// Start the scheduler.
			scheduler := gocron.NewScheduler(time.UTC)
			scheduler.StartAsync()
			ctx.Go(func(ctx *stopper.Context) error {
				<-ctx.Stopping()
				scheduler.Stop()
				log.Info("scheduler stopped")
				return nil
			})

			// Register this node and start heartbeat.
			hostname, err := os.Hostname()
			if err != nil {
				hostname = uuid.New().String()
				log.Warnf("unable to get hostname, using generated id: %s", hostname)
			}
			pid := os.Getpid()
			nodeID, err := store.RegisterNode(ctx, hostname, pid, http.Version)
			if err != nil {
				return errors.Wrap(err, "registering node")
			}
			log.Infof("registered node id=%d hostname=%s pid=%d version=%s", nodeID, hostname, pid, http.Version)

			// Deregister this node on shutdown.
			ctx.Go(func(ctx *stopper.Context) error {
				<-ctx.Stopping()
				cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := store.DeleteNode(cleanupCtx, nodeID); err != nil {
					log.Warnf("failed to deregister node on shutdown: %v", err)
				} else {
					log.Infof("deregistered node id=%d", nodeID)
				}
				return nil
			})

			if _, err := scheduler.Every(1).Minute().Do(func() {
				if ctx.Err() != nil {
					return
				}
				if err := store.Heartbeat(ctx, nodeID); err != nil {
					log.Errorf("node heartbeat: %v", err)
					return
				}
			}); err != nil {
				return errors.Wrap(err, "scheduling node heartbeat")
			}

			// Start the Prometheus http endpoint.
			httpServer, err := http.New(ctx, cfg, store, registry, scheduler)
			if err != nil {
				return err
			}
			if err := httpServer.Start(ctx); err != nil {
				return err
			}

			// Start the collector server.
			collServer, err := collector.New(cfg, store, adminConn, roConn, registry, scheduler)
			if err != nil {
				return err
			}
			if err := collServer.Start(ctx); err != nil {
				return err
			}

			// Start the server that manages the log scanners.
			scannerServer, err := scanner.New(cfg, store, registry, scheduler)
			if err != nil {
				return err
			}
			if err := scannerServer.Start(ctx); err != nil {
				return err
			}

			// Trap SIGHUP to force configuration reload.
			sigHup := make(chan os.Signal, 1)
			signal.Notify(sigHup, syscall.SIGHUP)
			ctx.Go(func(ctx *stopper.Context) error {
				defer close(sigHup)
				defer signal.Stop(sigHup)
				for {
					select {
					case <-ctx.Stopping():
						return nil
					case <-sigHup:
						// We try to refresh the various configurations.
						// If there are errors, we log them, but we
						// try to continue with the old configuration for
						// any server that fails.
						log.Info("Refreshing configuration on SIGHUP")
						if err := collServer.Refresh(ctx); err != nil {
							log.Errorf("refreshing metrics %q", err)
						}
						if err := scannerServer.Refresh(ctx); err != nil {
							log.Errorf("refreshing scanners %q", err)
						}
						if err := httpServer.Refresh(ctx); err != nil {
							log.Errorf("refreshing http  %q", err)
						}
						if err := roConn.Refresh(ctx); err != nil {
							log.Errorf("refreshing read only db connection %q", err)
						}
						if err := adminConn.Refresh(ctx); err != nil {
							log.Errorf("refreshing db connection %q", err)
						}
					}
				}
			})
			return ctx.Wait()
		},
	}
	f := c.Flags()
	f.BoolVar(&cfg.AllowUnsafeInternals, "allow-unsafe-internals", false, "set allow_unsafe_internals = true for read-only database connections")
	f.StringVar(&cfg.BindAddr, "bind-addr", "127.0.0.1:8888", "A network address and port to bind to")
	f.StringVar(&cfg.BindCert, "bind-cert", "",
		"Path to the  TLS certificate for the server")
	f.StringVar(&cfg.BindKey, "bind-key", "",
		"Path to the  TLS key for the server")
	f.StringVar(&cfg.CaCert, "ca-cert", "",
		"Path to the  CA certificate")
	f.DurationVar(&cfg.Refresh, "refresh", 5*time.Minute,
		"How ofter to refresh the configuration from the database.")
	f.StringVar(&cfg.Endpoint, "endpoint", "/_status/vars",
		"Endpoint for metrics.")
	f.BoolVar(&cfg.Inotify, "inotify", false, "enable inotify for scans")
	f.BoolVar(&cfg.Insecure, "insecure", false, "this flag must be set if no TLS configuration is provided")
	f.BoolVar(&cfg.ProcMetrics, "proc-metrics", false, "enable the collection of process metrics")
	f.BoolVar(&cfg.VisusMetrics, "visus-metrics", false, "enable the collection of visus metrics")
	f.StringVar(&cfg.URL, "url", "",
		"Connection URL, of the form: postgresql://[user[:passwd]@]host[:port]/[db][?parameters...]")
	f.StringVar(&cfg.Prometheus, "prometheus", "", "prometheus endpoint")
	f.BoolVar(&cfg.RewriteHistograms, "rewrite-histograms", false, "enable histogram rewriting")
	return c
}
