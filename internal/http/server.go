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

// Package http implements an http server to export metrics in Prometheus format.
package http

import (
	"context"
	"crypto/tls"
	"net/http"
	_ "net/http/pprof" // enabling debugging
	"net/url"
	"runtime/debug"

	"github.com/NYTimes/gziphandler"
	"github.com/cockroachdb/field-eng-powertools/stopper"
	"github.com/cockroachlabs/visus/internal/metric"
	"github.com/cockroachlabs/visus/internal/server"
	"github.com/cockroachlabs/visus/internal/store"
	"github.com/cockroachlabs/visus/internal/translator"
	"github.com/go-co-op/gocron"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/expfmt"
	log "github.com/sirupsen/logrus"
)

// Version of the visus binary, set using:
// `-ldflags="-X github.com/cockroachlabs/visus/internal/http.Version=..."`
var Version string

type serverImpl struct {
	clientTLSConfig *clientTLSConfig
	config          *server.Config
	httpServer      *http.Server
	keyPair         *keyPair
	metricsWriter   *metric.Writer
	registry        *prometheus.Registry
	scheduler       *gocron.Scheduler
	store           store.Store
}

var _ server.Server = &serverImpl{}

// New constructs a http server to server the metrics
func New(
	ctx context.Context,
	cfg *server.Config,
	st store.Store,
	registry *prometheus.Registry,
	scheduler *gocron.Scheduler,
) (server.Server, error) {
	tlsConfig := &tls.Config{}
	keyPair := &keyPair{}
	clientTLS := &clientTLSConfig{}
	if cfg.BindCert != "" && cfg.BindKey != "" {
		keyPair.certPath = cfg.BindCert
		keyPair.keyPath = cfg.BindKey
		keyPair.load()
		tlsConfig.GetCertificate = keyPair.getCertificateFunc()
	}
	if cfg.CaCert != "" {
		url, err := url.Parse(cfg.Prometheus)
		if err != nil {
			return nil, err
		}
		if url.Scheme == "https" {
			clientTLS.caPath = cfg.CaCert
			err := clientTLS.load()
			if err != nil {
				return nil, err
			}
		}
	}
	var writer *metric.Writer
	var err error
	if cfg.Prometheus != "" {
		writer, err = metric.NewWriter(cfg.Prometheus, tlsConfig, nil)
		if err != nil {
			return nil, err
		}
	}
	return &serverImpl{
		clientTLSConfig: clientTLS,
		config:          cfg,
		httpServer: &http.Server{
			Addr:      cfg.BindAddr,
			TLSConfig: tlsConfig,
		},
		keyPair:       keyPair,
		registry:      registry,
		scheduler:     scheduler,
		store:         st,
		metricsWriter: writer,
	}, nil
}

// Refresh implements server.Server
func (s *serverImpl) Refresh(ctx *stopper.Context) error {
	log.Info("Refreshing http server configuration")
	err := s.refresh(ctx)
	if err != nil {
		server.RefreshErrors.WithLabelValues("http_server").Inc()
		return err
	}
	server.RefreshCounts.WithLabelValues("http_server").Inc()
	return nil
}

// Start implements server.Server
func (s *serverImpl) Start(ctx *stopper.Context) error {
	_, err := s.scheduler.Every(s.config.Refresh).
		Do(func() {
			err := s.Refresh(ctx)
			if err != nil {
				log.Errorf("Error refreshing http server %s", err.Error())
			}
		})
	if err != nil {
		return err
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		if s.metricsWriter != nil {
			s.metricsWriter.Copy(ctx, w)
		}
		metrics, err := s.registry.Gather()
		if err != nil {
			s.errorResponse(w, "Error gathering metrics", err)
			return
		}
		for _, m := range metrics {
			_, err = expfmt.MetricFamilyToText(w, m)
			if err != nil {
				s.errorResponse(w, "Error gathering metrics", err)
			}
		}

	})

	http.Handle(s.config.Endpoint, gziphandler.GzipHandler(handler))
	s.debugInfo()
	ctx.Go(func(ctx *stopper.Context) error {
		var err error
		if !s.config.Insecure {
			log.Infof("Starting secure server: %v", s.config.BindAddr)
			// Passing empty strings, since we are using TLSConfig.GetCertificate. See
			// https://pkg.go.dev/net/http#Server.ListenAndServeTLS. Filenames containing a
			// certificate and matching private key for the server must be provided if neither the
			// Server's TLSConfig.Certificates nor TLSConfig.GetCertificate are populated
			err = s.httpServer.ListenAndServeTLS("", "")
		} else {
			log.Infof("Http server started: %v", s.config.BindAddr)
			err = s.httpServer.ListenAndServe()
		}
		if err != http.ErrServerClosed {
			log.Errorf("Error starting server: %s", err)
		}
		return nil
	})
	ctx.Go(func(ctx *stopper.Context) error {
		<-ctx.Stopping()
		if err := s.httpServer.Shutdown(ctx); err != nil {
			log.WithError(err).Error("did not shut down cleanly")
		} else {
			log.Info("Http server stopped")
		}
		return nil
	})
	return nil
}

func (s *serverImpl) errorResponse(w http.ResponseWriter, msg string, err error) {
	log.Errorf("%s: %s", msg, err.Error())
	w.WriteHeader(http.StatusInternalServerError)
	_, newErr := w.Write([]byte(err.Error()))
	if newErr != nil {
		log.Errorf("Error sending response to client %s", err)
	}
}

func (s *serverImpl) debugInfo() {
	if bi, ok := debug.ReadBuildInfo(); ok {
		labels := prometheus.Labels{
			"go_version": bi.GoVersion,
			"module":     bi.Path,
		}
		for _, setting := range bi.Settings {
			switch setting.Key {
			case "vcs.revision":
				labels["commit"] = setting.Value
			case "vcs.time":
				labels["build_time"] = setting.Value
			}
		}
		labels["version"] = Version
		g := prometheus.NewGauge(prometheus.GaugeOpts{
			Name:        "visus_info",
			Help:        "information about the visus binary",
			ConstLabels: labels,
		})
		g.Set(1)
		s.registry.Register(g)
	}
	g := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "visus_start_seconds",
		Help: "the wall time at which visus was started",
	})
	g.SetToCurrentTime()
	s.registry.Register(g)
}

// refresh the server configuration
func (s *serverImpl) refresh(ctx *stopper.Context) error {
	if err := s.keyPair.load(); err != nil {
		return err
	}
	if err := s.clientTLSConfig.load(); err != nil {
		return err
	}
	if s.metricsWriter == nil {
		return nil
	}
	if !s.config.RewriteHistograms {
		s.metricsWriter.SetTranslators(nil)
		return nil
	}
	translators, err := translator.Load(ctx, s.store)
	if err != nil {
		return err
	}
	s.metricsWriter.SetTranslators(translators)
	return nil
}
