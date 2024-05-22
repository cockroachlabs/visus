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

// Package store manages the configurations in the database.
package store

import (
	"context"
	_ "embed" // embedding sql statements
	"time"

	"github.com/cockroachlabs/visus/internal/database"
)

// Store provides the CRUD function to manage collection, histogram and scanner configurations.
type Store interface {
	// DeleteCollection deletes the collection with the given name from the store.
	DeleteCollection(ctx context.Context, name string) error
	// DeleteHistogram deletes the histogram with the given name from the store.
	DeleteHistogram(ctx context.Context, regex string) error
	// DeleteScan deletes the log target with the given name from the store.
	DeleteScan(ctx context.Context, name string) error
	// GetCollection returns the collection with the given name.
	GetCollection(ctx context.Context, name string) (*Collection, error)
	// GetCollectionNames returns the collection names present in the store.
	GetCollectionNames(ctx context.Context) ([]string, error)
	// GetHistogram returns the histogram with the given name.
	GetHistogram(ctx context.Context, name string) (*Histogram, error)
	// GetHistogramNames returns the histogram names present in the store.
	GetHistogramNames(ctx context.Context) ([]string, error)
	// GetMetrics returns the metrics associated to a collection.
	GetMetrics(ctx context.Context, name string) ([]Metric, error)
	// GetScan returns the log target with the given name.
	GetScan(ctx context.Context, name string) (*Scan, error)
	// GetScanNames returns the log target names present in the store.
	GetScanNames(ctx context.Context) ([]string, error)
	// GetScanPatterns returns the patterns associated to a log target.
	GetScanPatterns(ctx context.Context, name string) ([]Pattern, error)
	// Init initializes the schema in the database
	Init(ctx context.Context) error
	// IsMainNode returns true if the current node is the main node.
	// A main node is a node with max(id) in the cluster.
	IsMainNode(ctx context.Context, lastUpdated time.Time) (bool, error)
	// PutCollection adds a collection configuration to the database.
	PutCollection(ctx context.Context, collection *Collection) error
	// PutHistogram adds a histogram configuration to the database.
	PutHistogram(ctx context.Context, histogram *Histogram) error
	// PutScan adds a log target configuration to the database.
	PutScan(ctx context.Context, scan *Scan) error
}

type store struct {
	conn database.Connection
}

// New create a store
func New(conn database.Connection) Store {
	return &store{
		conn: conn,
	}
}

//go:embed sql/ddl.sql
var ddlStm string

//go:embed sql/mainNode.sql
var mainNodeStm string

// Init initializes the schema in the database
func (s *store) Init(ctx context.Context) error {
	_, err := s.conn.Exec(ctx, ddlStm)
	return err
}

// isMainNode returns true if the node id of the node is the max(id) in the cluster.
func (s *store) IsMainNode(ctx context.Context, lastUpdated time.Time) (bool, error) {
	var res *bool
	row := s.conn.QueryRow(ctx, mainNodeStm, lastUpdated)
	err := row.Scan(&res)
	if err != nil || res == nil {
		return false, err
	}
	return *res, nil
}
