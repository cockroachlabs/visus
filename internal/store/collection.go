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

package store

import (
	"context"
	_ "embed" // embedding sql statements
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

// Scope of the collector.
type Scope string

const (
	// Cluster scope collectors should only run on one node of the cluster.
	Cluster = Scope("cluster")
	// Node scope collectors run on all the nodes in the cluster.
	Node = Scope("node")
)

// Kind is the type of the metric.
type Kind string

const (
	// Counter is a metric value which can only increase or reset.
	Counter = Kind("counter")
	// Gauge is a number which can either go up or down.
	Gauge = Kind("gauge")
)

// A Metric represents a measurement for a specific property.
type Metric struct {
	Name string // Name of the metric, which represents the property being measured.
	Kind Kind   // Kind is the type of the metric.
	Help string // Help to used to describe the metric.
}

// Collection is a set of metrics that evaluated at the same time.
// This struct defines the configuration for the collection.
type Collection struct {
	Enabled      bool             // Enabled is true if the metrics needs to be collected.
	Frequency    pgtype.Interval  // Frequency determines how often the metrics are collected.
	Labels       []string         // Labels provide dimensions to the metrics.
	LastModified pgtype.Timestamp // LastModified the last time the collection was updated in the database.
	MaxResult    int              // Maximun number of results to be retrieved from the database.
	Metrics      []Metric         // Metrics available in this collection
	Name         string           // Name of the collection
	Query        string           // Query is the SQL query used to retrieve the metric values.
	Scope        Scope            // Scope of the collection (global vs local)
}

//go:embed sql/listCollections.sql
var listCollectionsStmt string

//go:embed sql/getCollection.sql
var getCollectionStmt string

//go:embed sql/getMetrics.sql
var getMetricsStmt string

//go:embed sql/upsertCollection.sql
var upsertCollectionStmt string

//go:embed sql/insertMetric.sql
var insertMetricStmt string

//go:embed sql/deleteCollection.sql
var deleteCollectionStmt string

//go:embed sql/deleteMetrics.sql
var deleteMetricsStmt string

// DeleteCollection removes a collection configuration and associated metrics from the database.
func (s *store) DeleteCollection(ctx context.Context, name string) error {
	txn, err := s.conn.Begin(ctx)
	if err != nil {
		log.Debugln(err)
		return err
	}
	defer func() {
		if err := txn.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			log.Errorf("rollback failed %s", err)
		}
	}()
	_, err = txn.Exec(ctx, deleteMetricsStmt, name)
	if err != nil {
		return errors.WithStack(err)
	}
	_, err = txn.Exec(ctx, deleteCollectionStmt, name)
	if err != nil {
		return errors.WithStack(err)
	}
	return txn.Commit(ctx)
}

// GetCollectionNames retrieves all the collection names stored in the database.
func (s *store) GetCollectionNames(ctx context.Context) ([]string, error) {
	rows, err := s.conn.Query(ctx, listCollectionsStmt)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	defer rows.Close()
	res := make([]string, 0)
	for rows.Next() {
		var name string
		err := rows.Scan(&name)
		if err != nil {
			return nil, errors.WithStack(err)
		}
		res = append(res, name)
	}
	return res, nil
}

// GetCollection retrieves the collection configuration from the database
func (s *store) GetCollection(ctx context.Context, name string) (*Collection, error) {
	collection := &Collection{}
	collRow := s.conn.QueryRow(ctx, getCollectionStmt, name)
	if err := collRow.Scan(
		&collection.Name, &collection.LastModified,
		&collection.Enabled, &collection.Scope, &collection.MaxResult,
		&collection.Frequency, &collection.Query, &collection.Labels); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.WithStack(err)
	}
	metrics, err := s.GetMetrics(ctx, name)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	collection.Metrics = metrics
	return collection, nil
}

// GetMetrics retrieves the configuration for the metrics associated to this collection.
func (s *store) GetMetrics(ctx context.Context, name string) ([]Metric, error) {
	metrics := make([]Metric, 0)
	rows, err := s.conn.Query(ctx, getMetricsStmt, name)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	defer rows.Close()
	for rows.Next() {
		metric := Metric{}
		err := rows.Scan(&metric.Name, &metric.Kind, &metric.Help)
		if err != nil {
			return nil, errors.WithStack(err)
		}
		metrics = append(metrics, metric)
	}
	return metrics, nil
}

// PutCollection adds a new collection configuration to the database.
// If a collection with the same name already exists, it is replaced.
func (s *store) PutCollection(ctx context.Context, collection *Collection) error {
	txn, err := s.conn.Begin(ctx)
	if err != nil {
		return errors.WithStack(err)
	}
	defer func() {
		if err := txn.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			log.Errorf("rollback failed %s", err)
		}
	}()
	_, err = txn.Exec(ctx, deleteMetricsStmt, collection.Name)
	if err != nil {
		return errors.WithStack(err)
	}
	_, err = txn.Exec(ctx, upsertCollectionStmt,
		collection.Name, collection.Enabled, collection.Scope, collection.MaxResult,
		collection.Frequency, collection.Query, collection.Labels)
	if err != nil {
		return errors.WithStack(err)
	}
	for _, metric := range collection.Metrics {
		_, err = txn.Exec(ctx, insertMetricStmt, collection.Name, metric.Name,
			metric.Kind, metric.Help)
		if err != nil {
			return errors.WithStack(err)
		}
	}
	return txn.Commit(ctx)
}

// String implements fmt.Stringer
func (c *Collection) String() string {
	return fmt.Sprintf(`Collection: %s
Enabled:    %t
Updated:    %s
Labels:     %s
Query:      %s
MaxResults: %d
Frequency:  %d seconds
Metrics:    %v`,
		c.Name, c.Enabled, c.LastModified.Time, strings.Join(c.Labels, ","),
		c.Query, c.MaxResult,
		c.Frequency.Microseconds/(1000*1000), c.Metrics)
}
