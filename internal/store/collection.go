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

// Package store manages configurations in the database.
package store

import (
	"context"
	_ "embed" // embedding sql statements
	"fmt"
	"strings"

	"github.com/cockroachlabs/visus/internal/database"
	"github.com/jackc/pgtype"
	log "github.com/sirupsen/logrus"
)

// Scope of the collector.
type Scope string

const (
	// Global collectors should only run on one node of the cluster.
	Global = Scope("global")
	// Local collectors run on all the nodes in the cluster.
	Local = Scope("local")
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

//go:embed sql/ddl.sql
var ddl string

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

// Init initializes the connection to the database
func Init(ctx context.Context, conn database.PgxPool) error {
	_, err := conn.Exec(ctx, ddl)
	return err
}

// DeleteCollection removes a collection configuration and associated metrics from the database.
func DeleteCollection(ctx context.Context, pool database.PgxPool, name string) error {
	txn, err := pool.Begin(ctx)
	if err != nil {
		log.Debugln(err)
		return err
	}
	defer txn.Commit(ctx)
	_, err = txn.Exec(ctx, deleteMetricsStmt, name)
	if err != nil {
		log.Errorf("delete metrics error:%s", err)
		txn.Rollback(ctx)
		return err
	}
	_, err = txn.Exec(ctx, deleteCollectionStmt, name)
	if err != nil {
		log.Errorf("delete metrics error:%s", err)
		txn.Rollback(ctx)
		return err
	}
	return nil
}

// GetCollectionNames retrieves all the collection names stored in the database.
func GetCollectionNames(ctx context.Context, pool database.PgxPool) ([]string, error) {
	rows, err := pool.Query(ctx, listCollectionsStmt)
	if err != nil {
		log.Errorf("GetCollectionNames %s ", err.Error())
		return nil, err
	}
	defer rows.Close()
	res := make([]string, 0)

	for rows.Next() {
		var name string
		err := rows.Scan(&name)
		if err != nil {
			log.Debugln(err)
			return nil, err
		}
		res = append(res, name)
	}
	return res, nil
}

// GetCollection retrieves the collection configuration from the database
func GetCollection(ctx context.Context, pool database.PgxPool, name string) (*Collection, error) {
	collection := &Collection{}
	collRows, err := pool.Query(ctx, getCollectionStmt, name)
	if err != nil {
		log.Errorf("GetCollection %s ", err.Error())
		return nil, err
	}
	defer collRows.Close()
	if collRows.Next() {
		err := collRows.Scan(
			&collection.Name, &collection.LastModified,
			&collection.Enabled, &collection.Scope, &collection.MaxResult,
			&collection.Frequency, &collection.Query, &collection.Labels)
		if err != nil {
			log.Debugln(err)
			return nil, err
		}
		metrics, err := GetMetrics(ctx, pool, name)
		if err != nil {
			log.Debugln(err)
			return nil, err
		}
		collection.Metrics = metrics
	} else {
		return nil, nil
	}
	return collection, nil
}

// GetMetrics retrieves the configuration for the metrics associated to this collection.
func GetMetrics(ctx context.Context, pool database.PgxPool, name string) ([]Metric, error) {
	metrics := make([]Metric, 0)
	rows, err := pool.Query(ctx, getMetricsStmt, name)
	if err != nil {
		log.Errorf("GetMetrics %s ", err.Error())
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		metric := Metric{}
		err := rows.Scan(&metric.Name, &metric.Kind, &metric.Help)
		if err != nil {
			log.Debugln(err)
			return nil, err
		}
		metrics = append(metrics, metric)
	}
	return metrics, nil
}

// PutCollection adds a new collection configuration to the database.
// If a collection with the same name already exists, it is replaced.
func PutCollection(ctx context.Context, pool database.PgxPool, collection *Collection) error {
	log.Debugf("%+v", collection)
	txn, err := pool.Begin(ctx)
	if err != nil {
		log.Debugln(err)
		return err
	}
	defer txn.Commit(ctx)
	_, err = txn.Exec(ctx, deleteMetricsStmt, collection.Name)
	if err != nil {
		log.Errorf("delete metrics error:%s", err)
		txn.Rollback(ctx)
		return err
	}
	_, err = txn.Exec(ctx, upsertCollectionStmt,
		collection.Name, collection.Scope, collection.MaxResult,
		collection.Frequency, collection.Query, collection.Labels)
	if err != nil {
		log.Errorf("upsert collection error:%s, %s", upsertCollectionStmt, err)
		txn.Rollback(ctx)
		return err
	}
	for _, metric := range collection.Metrics {
		_, err = txn.Exec(ctx, insertMetricStmt, collection.Name, metric.Name,
			metric.Kind, metric.Help)
		if err != nil {
			log.Errorf("insert metric error:%s", err)
			txn.Rollback(ctx)
			return err
		}
	}
	return nil
}

// String implements fmt.Stringer
// TODO (silvano): clean up
func (c *Collection) String() string {
	return fmt.Sprintf(`Collection: %s
Updated:    %s
Labels:     %s
Query:      %s
MaxResults: %d
Frequency:  %d seconds
Metrics:    %v`,
		c.Name, c.LastModified.Time, strings.Join(c.Labels, ","),
		c.Query, c.MaxResult,
		c.Frequency.Microseconds/(1000*1000), c.Metrics)
}
