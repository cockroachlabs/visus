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
	"crypto/tls"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	log "github.com/sirupsen/logrus"
)

// FactoryConfig is the configuration struct for creating database connections.
type FactoryConfig struct {
	readOnly           bool
	ReloadCertificates bool
	ConnectionUrl      string
}

// Factory creates database connections
type Factory interface {
	//New creates a new read/write connection to the database.
	New(ctx context.Context, connectionUrl string) (Connection, error)
	//NewWithConfig creates a new read/write connection to the database. Config must be [FactoryConfig]
	NewWithConfig(ctx context.Context, opts *FactoryConfig) (Connection, error)
	//ReadOnly creates a read only connection to the database.
	ReadOnly(ctx context.Context, connectionUrl string) (Connection, error)
	//ReadOnlyWithConfig creates a read only connection to the database. Config must be [FactoryConfig]
	ReadOnlyWithConfig(ctx context.Context, opts *FactoryConfig) (Connection, error)
}

type factory struct {
}

// DefaultFactory creates a pool of PGX connections.
var DefaultFactory = &factory{}

// Connection defines the methods to access the database.
type Connection interface {
	Exec(ctx context.Context, sql string, arguments ...interface{}) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults
	Begin(ctx context.Context) (pgx.Tx, error)
	BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error)
	Reset()
}

// New creates a new read/write connection to the database.
// It waits until a connection can be established, or the context has been cancelled.
func (f factory) New(ctx context.Context, connectionUrl string) (Connection, error) {
	return f.new(ctx, &FactoryConfig{
		readOnly:      false,
		ConnectionUrl: connectionUrl,
	})
}

// NewWithConfig creates a new read/write connection to the database. Config must be [FactoryConfig]
// It waits until a connection can be established, or the context has been cancelled.
func (f factory) NewWithConfig(ctx context.Context, cfg *FactoryConfig) (Connection, error) {
	cfg.readOnly = false
	return f.new(ctx, cfg)
}

// ReadOnly creates a new connection to the database with follower reads
// It waits until a connection can be established, or the context has been cancelled.
func (f factory) ReadOnly(ctx context.Context, connectionUrl string) (Connection, error) {
	return f.new(ctx, &FactoryConfig{
		readOnly:      true,
		ConnectionUrl: connectionUrl,
	})
}

// ReadOnlyWithConfig creates a new connection to the database with follower reads. Config must be [FactoryConfig]
// It waits until a connection can be established, or the context has been cancelled.
func (f factory) ReadOnlyWithConfig(ctx context.Context, cfg *FactoryConfig) (Connection, error) {
	cfg.readOnly = true
	return f.new(ctx, cfg)
}

func (f factory) new(ctx context.Context, cfg *FactoryConfig) (Connection, error) {
	var conn *pgxpool.Pool
	sleepTime := int64(5)
	if cfg.ConnectionUrl == "" {
		return nil, errors.New("URL is required")
	}
	connectionUrl := cfg.ConnectionUrl
	poolConfig, err := pgxpool.ParseConfig(connectionUrl)
	if err != nil {
		log.Fatal(err)
	}
	if cfg.readOnly {
		poolConfig.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
			log.Debug("setting up a read only session")
			_, err := conn.Exec(ctx, "set session default_transaction_use_follower_reads = true;")
			return err
		}
	}
	if cfg.ReloadCertificates && poolConfig.ConnConfig.TLSConfig != nil {
		loadCertFunc := func(_ *tls.CertificateRequestInfo) (*tls.Certificate, error) {
			log.Debug("loading pool certificates")
			c, parseErr := pgxpool.ParseConfig(connectionUrl)
			if parseErr != nil {
				log.Error(parseErr)
				return nil, parseErr
			}
			if len(c.ConnConfig.TLSConfig.Certificates) > 0 {
				return &c.ConnConfig.TLSConfig.Certificates[0], nil
			}
			return nil, errors.New("pool certificate not found")
		}

		poolConfig.ConnConfig.TLSConfig.GetClientCertificate = loadCertFunc
		for _, fb := range poolConfig.ConnConfig.Fallbacks {
			if fb.TLSConfig != nil {
				fb.TLSConfig.GetClientCertificate = loadCertFunc
			}
		}
	}
	for {
		conn, err = pgxpool.NewWithConfig(ctx, poolConfig)
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
