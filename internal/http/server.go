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
	"net/http"
	"time"

	"github.com/NYTimes/gziphandler"
	"github.com/cockroachlabs/visus/internal/server"
	"github.com/cockroachlabs/visus/internal/store"
	"github.com/cockroachlabs/visus/internal/translator"
	"github.com/go-co-op/gocron"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/expfmt"
	log "github.com/sirupsen/logrus"
)

type serverImpl struct {
	config      *server.Config
	httpServer  *http.Server
	translators []translator.Translator
	store       store.Store
	scheduler   *gocron.Scheduler
	registry    *prometheus.Registry
}

// New constructs a http server to server the metrics
func New(
	ctx context.Context, cfg *server.Config, st store.Store, registry *prometheus.Registry,
) (server.Server, error) {
	httpServer := &http.Server{
		Addr: cfg.BindAddr,
	}
	server := &serverImpl{
		config:      cfg,
		translators: make([]translator.Translator, 0),
		httpServer:  httpServer,
		store:       st,
		scheduler:   gocron.NewScheduler(time.UTC),
		registry:    registry,
	}
	return server, nil
}

func (s *serverImpl) Refresh(ctx context.Context) error {
	if !s.config.RewriteHistograms {
		return nil
	}
	names, err := s.store.GetHistogramNames(ctx)
	if err != nil {
		return err
	}
	s.translators = nil
	for _, name := range names {
		histogram, err := s.store.GetHistogram(ctx, name)
		if err != nil {
			return err
		}
		hnew, err := translator.New(*histogram)
		if err != nil {
			log.Errorf("Error translating histograms", err)
			continue
		}
		s.translators = append(s.translators, hnew)
	}
	return nil
}

// Start the server and wait for new connections.
// It uses the default prometheus handler to return the metric value to the caller.
func (s *serverImpl) Start(ctx context.Context) error {
	s.scheduler.Every(s.config.Refresh).
		Do(func() {
			s.Refresh(ctx)
		})
	s.scheduler.StartAsync()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		if s.config.Prometheus != "" {
			tlsConfig, err := s.config.GetTLSClientConfig()
			if err != nil {
				s.errorResponse(w, "Error setting up secure connection", err)
				return
			}
			metricsIn, err := ReadMetrics(ctx, s.config.Prometheus, tlsConfig)
			if err != nil {
				log.Error(err)
			}
			WriteMetrics(ctx, metricsIn, s.config.RewriteHistograms, s.translators, w)
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
	go func() {
		var err error
		if !s.config.Insecure {
			log.Infof("Starting secure server: %v", s.config.BindAddr)
			err = s.httpServer.ListenAndServeTLS(s.config.BindCert, s.config.BindKey)
		} else {
			log.Infof("Starting server: %v", s.config.BindAddr)
			err = s.httpServer.ListenAndServe()
		}
		if err != http.ErrServerClosed {
			log.Fatal("Error starting server: ", err)
		}
	}()
	return nil
}

func (s *serverImpl) Shutdown(ctx context.Context) error {
	log.Info("Shutting down ")
	return s.httpServer.Shutdown(ctx)
}

func (s *serverImpl) errorResponse(w http.ResponseWriter, msg string, err error) {
	log.Errorf("%s: %s", msg, err.Error())
	w.WriteHeader(http.StatusInternalServerError)
	_, newErr := w.Write([]byte(err.Error()))
	if newErr != nil {
		log.Errorf("Error sending response to client %s", err)
	}
}
