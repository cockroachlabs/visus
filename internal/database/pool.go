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

// Package database defines the interface to the database.
package database

import (
	"context"
	"time"

	"github.com/jackc/pgconn"
	"github.com/jackc/pgx/v4"
	"github.com/jackc/pgx/v4/pgxpool"
	log "github.com/sirupsen/logrus"
)

// Factory creates database connections
type Factory interface {
	//New creates a new connection to the database.
	New(ctx context.Context, URL string) (Connection, error)
	//ReadOnly creates a read only connection to the database.
	ReadOnly(ctx context.Context, URL string) (Connection, error)
}

type factory struct {
}

// DefaultFactory creates a pool of PGX connections.
var DefaultFactory = &factory{}

// Connection defines the methods to access the database.
type Connection interface {
	Exec(ctx context.Context, sql string, arguments ...interface{}) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row
	QueryFunc(ctx context.Context, sql string, args []interface{}, scans []interface{}, f func(pgx.QueryFuncRow) error) (pgconn.CommandTag, error)
	SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults
	Begin(ctx context.Context) (pgx.Tx, error)
	BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error)
	BeginFunc(ctx context.Context, f func(pgx.Tx) error) error
	BeginTxFunc(ctx context.Context, txOptions pgx.TxOptions, f func(pgx.Tx) error) error
}

// New creates a new connection to the database.
// It waits until a connection can be established, or the the context has been cancelled.
func (f factory) New(ctx context.Context, URL string) (Connection, error) {
	return f.new(ctx, URL, false)
}

// ReadOnly creates a new connection to the database with follower reads
// It waits until a connection can be established, or the the context has been cancelled.
func (f factory) ReadOnly(ctx context.Context, URL string) (Connection, error) {
	return f.new(ctx, URL, true)
}

func (f factory) new(ctx context.Context, URL string, ro bool) (Connection, error) {
	var conn *pgxpool.Pool
	sleepTime := int64(5)
	poolConfig, err := pgxpool.ParseConfig(URL)
	if err != nil {
		log.Fatal(err)
	}
	if ro {
		poolConfig.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
			log.Info("setting up a read only session")
			_, err := conn.Exec(ctx, "set session default_transaction_use_follower_reads = true;")
			return err
		}
	}
	for {
		conn, err = pgxpool.ConnectConfig(ctx, poolConfig)
		if err != nil {
			log.Error(err)
			log.Warnf("Unable to connect to the db. Retrying in %d seconds", sleepTime)
			err := sleep(ctx, sleepTime)
			if err != nil {
				return nil, err
			}
		} else {
			break
		}
		if sleepTime < int64(60) {
			sleepTime += int64(5)
		}
	}
	return conn, nil
}

func sleep(ctx context.Context, seconds int64) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(time.Duration(seconds) * time.Second):
		return nil
	}
}
