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
	"errors"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cockroachlabs/visus/internal/database"
	"github.com/cockroachlabs/visus/internal/http"
	"github.com/cockroachlabs/visus/internal/metric"
	"github.com/cockroachlabs/visus/internal/server"
	"github.com/cockroachlabs/visus/internal/store"
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
			ctx := cmd.Context()
			if (cfg.BindCert == "" || cfg.BindKey == "") && !cfg.Insecure {
				return errors.New("--insecure must be specfied if certificates and private key are missing")
			}
			if cfg.URL == "" {
				return errors.New("--url must be specified")
			}
			conn, err := database.New(ctx, cfg.URL)
			if err != nil {
				return err
			}
			store := store.New(conn)
			roConn, err := database.ReadOnly(ctx, cfg.URL)
			if err != nil {
				return err
			}

			registry := prometheus.NewRegistry()

			// Run the httpServer in a separate context, so that we can
			// control the shutdown process.
			httpServer, err := http.New(ctx, cfg, store, registry)
			if err != nil {
				return err
			}
			defer httpServer.Stop(ctx)
			err = httpServer.Start(ctx)
			if err != nil {
				return err
			}

			metricServer := metric.New(ctx, cfg, store, roConn, registry)
			err = metricServer.Start(ctx)
			if err != nil {
				return err
			}
			defer metricServer.Stop(ctx)

			signalChan := make(chan os.Signal, 1)
			signal.Notify(signalChan, syscall.SIGHUP)

			go func() {
				for {
					s := <-signalChan
					switch s {
					case syscall.SIGHUP:
						log.Info("Refreshing configuration")
						if err := metricServer.Refresh(ctx); err != nil {
							log.Errorf("refreshing metrics %q", err)
						}
						if err := httpServer.Refresh(ctx); err != nil {
							log.Errorf("refreshing http  %q", err)
						}
						if err := roConn.Refresh(ctx); err != nil {
							log.Errorf("refreshing read only db connection %q", err)
						}
						if err := conn.Refresh(ctx); err != nil {
							log.Errorf("refreshing db connection %q", err)
						}
					}
				}
			}()

			// Wait to be shut down.
			<-ctx.Done()
			return nil
		},
	}
	f := c.Flags()
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
