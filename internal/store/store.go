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

	"github.com/cockroachlabs/visus/internal/database"
)

// Store provides the CRUD function to manage collection and histogram configurations.
type Store interface {
	// DeleteCollection deletes the collection with the given name from the store.
	DeleteCollection(ctx context.Context, name string) error
	// DeleteHistogram deletes the histogram with the given name from the store.
	DeleteHistogram(ctx context.Context, regex string) error
	// GetCollection returns the collection with the given name.
	GetCollection(ctx context.Context, name string) (*Collection, error)
	// GetCollectionNames returns the collection names present in the store.
	GetCollectionNames(ctx context.Context) ([]string, error)
	// GetHistograms returns the histograms from the database.
	GetHistogram(ctx context.Context, name string) (*Histogram, error)
	// GetHistograms returns the histograms from the database.
	GetHistogramNames(ctx context.Context) ([]string, error)
	// GetMetrics returns the metrics associated to a collection.
	GetMetrics(ctx context.Context, name string) ([]Metric, error)
	// GetPool returns the underlying database connection pool
	GetPool() database.PgxPool
	// Init initializes the schema in the database
	Init(ctx context.Context) error
	// PutCollection adds a collection configuration to the database.
	PutCollection(ctx context.Context, collection *Collection) error
	// PutHistogram adds a histogram configuration to the database.
	PutHistogram(ctx context.Context, histogram *Histogram) error
}

type store struct {
	pool database.PgxPool
}

// New create a store
func New(pool database.PgxPool) Store {
	return &store{
		pool: pool,
	}
}

// GetPool returns the underlying database connection pool
func (s *store) GetPool() database.PgxPool {
	return s.pool
}

//go:embed sql/ddl.sql
var ddl string

// Init initializes the schema in the database
func (s *store) Init(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, ddl)
	return err
}
