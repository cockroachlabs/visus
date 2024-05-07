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

// Package database defines the interface to the database.
package database

import (
	"context"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Pool provides a set of connections to the target database.
type Pool struct {
	readOnly bool
	URL      string
	mu       struct {
		sync.RWMutex
		pool *pgxpool.Pool
	}
}

var _ Connection = &Pool{}

// Begin implements Connection.
func (p *Pool) Begin(ctx context.Context) (pgx.Tx, error) {
	return p.get().Begin(ctx)
}

// BeginTx implements Connection.
func (p *Pool) BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error) {
	return p.get().BeginTx(ctx, txOptions)
}

// Exec implements Connection.
func (p *Pool) Exec(
	ctx context.Context, sql string, args ...interface{},
) (pgconn.CommandTag, error) {
	return p.get().Exec(ctx, sql, args...)
}

// Query implements Connection.
func (p *Pool) Query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error) {
	return p.get().Query(ctx, sql, args...)
}

// QueryRow implements Connection.
func (p *Pool) QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row {
	return p.get().QueryRow(ctx, sql, args...)
}

// Refresh creates a new pool, and reloads the certificates, if applicable.
// It is typically called when the process receives a SIGHUP.
func (p *Pool) Refresh(ctx context.Context) error {
	var pool *pgxpool.Pool
	var err error
	// We don't lock the pool until we get the new one.
	// Existing clients may still use the current pool.
	if pool, err = pgxPool(ctx, p.URL, p.readOnly); err != nil {
		return err
	}
	p.mu.Lock()
	old := p.mu.pool
	p.mu.pool = pool
	p.mu.Unlock()
	// Close the old pool
	old.Close()
	return nil

}

// SendBatch implements Connection.
func (p *Pool) SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults {
	return p.get().SendBatch(ctx, b)
}

// get returns the underlying pgxpool.Pool
func (p *Pool) get() *pgxpool.Pool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.mu.pool
}
