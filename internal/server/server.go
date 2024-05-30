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

// Package server manages access to a resource
package server

import (
	"github.com/cockroachdb/field-eng-powertools/stopper"
	"github.com/prometheus/client_golang/prometheus"
)

// Common server metrics.
var (
	labels = []string{"server"}
	// RefreshCounts tracks the number of times Refresh are called for a server.
	RefreshCounts = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "visus_refresh_counts",
			Help: "number of times refresh was executed",
		},
		labels,
	)
	// RefreshErrors tracks the number of times Refresh failed for a server.
	RefreshErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "visus_refresh_errors",
			Help: "number of times refresh failed",
		},
		labels,
	)
	collectors = []prometheus.Collector{RefreshCounts, RefreshErrors}
)

// The Server interface manages the lifecycle of a process that controls access to a resource.
// Its configuration can be refreshed.
// It uses the stopper interface to manage graceful shutdown.
type Server interface {
	Start(ctx *stopper.Context) error
	Refresh(ctx *stopper.Context) error
}

// RegisterMetrics register the server metrics to the named Prometheus registry.
func RegisterMetrics(registry *prometheus.Registry) error {
	for _, coll := range collectors {
		err := registry.Register(coll)
		if err != nil {
			return err
		}
	}
	return nil
}
