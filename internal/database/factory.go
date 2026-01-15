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

package database

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	log "github.com/sirupsen/logrus"
)

// New creates a new connection to the database.
// It waits until a connection can be established, or that the context has been cancelled.
func New(ctx context.Context, URL string) (*Pool, error) {
	return new(ctx, URL, false /* readOnly */, false /* allowUnsafeInternals */)
}

// ReadOnly creates a new connection to the database with follower reads.
// It waits until a connection can be established, or that the context has been cancelled.
// If allowUnsafeInternals is true, sets allow_unsafe_internals = true on the connection.
func ReadOnly(ctx context.Context, URL string, allowUnsafeInternals bool) (*Pool, error) {
	return new(ctx, URL, true /* readOnly */, allowUnsafeInternals)
}

func new(ctx context.Context, URL string, ro bool, allowUnsafeInternals bool) (*Pool, error) {
	pool, err := pgxPool(ctx, URL, ro, allowUnsafeInternals)
	if err != nil {
		return nil, err
	}
	res := &Pool{
		URL:                  URL,
		readOnly:             ro,
		allowUnsafeInternals: allowUnsafeInternals,
	}
	res.mu.pool = pool
	return res, nil
}

func pgxPool(
	ctx context.Context, URL string, ro bool, allowUnsafeInternals bool,
) (*pgxpool.Pool, error) {
	var pool *pgxpool.Pool
	sleepTime := int64(5)
	poolConfig, err := pgxpool.ParseConfig(URL)
	if err != nil {
		log.Fatal(err)
	}
	if ro || allowUnsafeInternals {
		poolConfig.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
			if ro {
				log.Debug("setting up a read only session")
				if _, err := conn.Exec(ctx, "set session default_transaction_use_follower_reads = true;"); err != nil {
					return err
				}
			}
			if allowUnsafeInternals {
				log.Debug("setting allow_unsafe_internals = true")
				if _, err := conn.Exec(ctx, "set allow_unsafe_internals = true;"); err != nil {
					return err
				}
			}
			return nil
		}
	}
	for {
		pool, err = pgxpool.NewWithConfig(ctx, poolConfig)
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
	log.Debugf("new pool %s (readonly: %t)", poolConfig.ConnString(), ro)
	return pool, err
}

func sleep(ctx context.Context, seconds int64) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(time.Duration(seconds) * time.Second):
		return nil
	}
}
