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
	"fmt"
	"sync"
	"time"

	"github.com/cockroachlabs/visus/internal/database"
	"github.com/google/uuid"
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
	// IsLeader returns true if this node is the elected leader.
	// refresh is the heartbeat interval; nodes that haven't heartbeated
	// within this window are considered inactive. The lease duration is
	// derived as 2x refresh.
	IsLeader(ctx context.Context, refresh time.Duration) (bool, error)
	// CleanupStaleNodes removes nodes that haven't heartbeated in 24 hours.
	CleanupStaleNodes(ctx context.Context) error
	// PutCollection adds a collection configuration to the database.
	PutCollection(ctx context.Context, collection *Collection) error
	// PutHistogram adds a histogram configuration to the database.
	PutHistogram(ctx context.Context, histogram *Histogram) error
	// PutScan adds a log target configuration to the database.
	PutScan(ctx context.Context, scan *Scan) error
}

type store struct {
	conn database.Connection

	mu struct {
		sync.Mutex
		nodeID int64
		nonce  uuid.UUID
	}
}

// New creates a store. The CockroachDB node ID is resolved lazily on first use.
func New(conn database.Connection) Store {
	return &store{
		conn: conn,
	}
}

// NewWithNodeID creates a store with an explicit node ID, useful for testing.
func NewWithNodeID(conn database.Connection, nodeID int64) Store {
	s := &store{
		conn: conn,
	}
	s.mu.nodeID = nodeID
	return s
}

// lockedGetNodeID returns the CockroachDB node ID, resolving it on the first call.
// Must be called with s.mu held.
func (s *store) lockedGetNodeID(ctx context.Context) (int64, error) {
	if s.mu.nodeID != 0 {
		return s.mu.nodeID, nil
	}
	row := s.conn.QueryRow(ctx, "SELECT node_id::INT FROM [SHOW node_id]")
	if err := row.Scan(&s.mu.nodeID); err != nil {
		return 0, fmt.Errorf("resolving node ID: %w", err)
	}
	return s.mu.nodeID, nil
}

//go:embed sql/ddl.sql
var ddlStm string

//go:embed sql/cleanupNodes.sql
var cleanupNodesStm string

//go:embed sql/touchNode.sql
var touchNodeStm string

//go:embed sql/renewLease.sql
var renewLeaseStm string

//go:embed sql/claimLeader.sql
var claimLeaderStm string

// Init initializes the schema in the database
func (s *store) Init(ctx context.Context) error {
	_, err := s.conn.Exec(ctx, ddlStm)
	return err
}

// IsLeader returns true if this node is the elected leader.
func (s *store) IsLeader(ctx context.Context, refresh time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lockedIsLeader(ctx, refresh)
}

// lockedIsLeader implements leader election. Must be called with s.mu held.
func (s *store) lockedIsLeader(ctx context.Context, refresh time.Duration) (bool, error) {
	nodeID, err := s.lockedGetNodeID(ctx)
	if err != nil {
		return false, err
	}
	// Upsert heartbeat
	if _, err := s.conn.Exec(ctx, touchNodeStm, nodeID); err != nil {
		return false, err
	}
	leaseDuration := 2 * refresh
	// Try to renew our lease and read the current leader state in one
	// round-trip. The query returns (isLeader, lease_valid).
	var isLeader, leaseValid bool
	row := s.conn.QueryRow(ctx, renewLeaseStm, leaseDuration, nodeID, s.mu.nonce)
	if err := row.Scan(&isLeader, &leaseValid); err != nil {
		return false, err
	}
	// Only attempt to claim if the lease has actually expired.
	// When another node holds a valid lease, skip the claim entirely.
	if !isLeader && !leaseValid {
		var nonce *uuid.UUID
		row := s.conn.QueryRow(ctx, claimLeaderStm, leaseDuration, nodeID)
		if err := row.Scan(&nonce); err != nil {
			nonce = nil
		}
		if nonce != nil {
			s.mu.nonce = *nonce
			isLeader = true
		}
	}
	return isLeader, nil
}

// CleanupStaleNodes removes nodes that haven't heartbeated in 24 hours.
func (s *store) CleanupStaleNodes(ctx context.Context) error {
	_, err := s.conn.Exec(ctx, cleanupNodesStm)
	return err
}
