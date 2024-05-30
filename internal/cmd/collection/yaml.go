// Copyright 2024 Cockroach Labs Inc.
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

// Package collection defines the sub command to run visus collection utilities.
package collection

import (
	"errors"
	"strings"

	"github.com/cockroachlabs/visus/internal/store"
	"github.com/creasty/defaults"
	"github.com/jackc/pgx/v5/pgtype"
	"gopkg.in/yaml.v3"
)

// metricDef yaml metric definition
type metricDef struct {
	Name string
	Kind string
	Help string
}

// config yaml collection definition
type config struct {
	Name       string
	Frequency  int
	MaxResults int
	Enabled    bool
	Query      string
	Labels     []string
	Metrics    []metricDef
	Scope      string `default:"node"`
}

func marshal(collection *store.Collection) ([]byte, error) {
	metrics := make([]metricDef, 0)
	for _, m := range collection.Metrics {
		metric := metricDef{
			Name: m.Name,
			Kind: string(m.Kind),
			Help: m.Help,
		}
		metrics = append(metrics, metric)
	}
	config := &config{
		Name:       collection.Name,
		Frequency:  int(collection.Frequency.Microseconds / microsecondsPerSecond),
		MaxResults: collection.MaxResult,
		Enabled:    collection.Enabled,
		Query:      collection.Query,
		Labels:     collection.Labels,
		Metrics:    metrics,
		Scope:      string(collection.Scope),
	}
	return yaml.Marshal(config)
}

func unmarshal(data []byte) (*store.Collection, error) {
	config := &config{}
	err := yaml.Unmarshal(data, &config)
	if err != nil {
		return nil, err
	}
	if err := defaults.Set(config); err != nil {
		return nil, err
	}
	metrics := make([]store.Metric, 0)
	for _, m := range config.Metrics {
		metrics = append(metrics, store.Metric{
			Name: m.Name,
			Kind: store.Kind(m.Kind),
			Help: m.Help,
		})
	}
	var scope store.Scope
	switch strings.ToLower(config.Scope) {
	case "node":
		scope = store.Node
	case "cluster":
		scope = store.Cluster
	default:
		return nil, errors.New("invalid scope")
	}
	if config.Query == "" {
		return nil, errors.New("query must be specified")
	}
	if config.Name == "" {
		return nil, errors.New("name must be specified")
	}
	return &store.Collection{
		Name:      config.Name,
		Enabled:   config.Enabled,
		Scope:     scope,
		MaxResult: config.MaxResults,
		Frequency: pgtype.Interval{
			Valid:        true,
			Microseconds: int64(config.Frequency) * microsecondsPerSecond,
		},
		Query:   config.Query,
		Labels:  config.Labels,
		Metrics: metrics,
	}, nil
}
