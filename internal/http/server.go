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

	"github.com/cockroachlabs/visus/internal/config"
	"github.com/cockroachlabs/visus/internal/server"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"
)

type serverImpl struct {
	config     *config.Config
	httpServer *http.Server
}

// New constructs a http server to server the metrics
func New(ctx context.Context, cfg *config.Config) (server.Server, error) {

	httpServer := &http.Server{
		Addr: cfg.BindAddr,
	}
	server := &serverImpl{
		config:     cfg,
		httpServer: httpServer,
	}
	tlsConfig, err := cfg.TLSConfig()
	if err != nil {
		return nil, err
	}
	httpServer.TLSConfig = tlsConfig

	log.Infof("Starting server. Listening on %s", cfg.BindAddr)

	return server, nil
}

func (s *serverImpl) Start(ctx context.Context) error {
	http.Handle(s.config.Endpoint, promhttp.Handler())
	go func() {
		err := s.httpServer.ListenAndServe()
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
