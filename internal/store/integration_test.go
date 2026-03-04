// Copyright 2026 Cockroach Labs Inc.
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

//go:build integration

package store

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/cockroachlabs/visus/internal/database"
	"github.com/stretchr/testify/require"
)

// getLeaseExpiry returns the current lease_expires_at from the leader table.
func getLeaseExpiry(ctx context.Context, t *testing.T, conn database.Connection) *time.Time {
	t.Helper()
	var leaseExpiresAt *time.Time
	row := conn.QueryRow(ctx, "SELECT lease_expires_at FROM _visus.leader")
	require.NoError(t, row.Scan(&leaseExpiresAt))
	return leaseExpiresAt
}

// expireLease forces the leader lease to expire.
func expireLease(ctx context.Context, t *testing.T, conn database.Connection) {
	t.Helper()
	_, err := conn.Exec(ctx,
		"UPDATE _visus.leader SET lease_expires_at = current_timestamp() - INTERVAL '1 second'")
	require.NoError(t, err)
}

// deleteNode removes a node from the node table.
func deleteNode(ctx context.Context, t *testing.T, conn database.Connection, nodeID int64) {
	t.Helper()
	_, err := conn.Exec(ctx, "DELETE FROM _visus.node WHERE id = $1", nodeID)
	require.NoError(t, err)
}

func TestIntegrationElection(t *testing.T) {
	url := os.Getenv("VISUS_TEST_DB_URL")
	if url == "" {
		t.Skip("VISUS_TEST_DB_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conn, err := database.New(ctx, url)
	require.NoError(t, err)
	s := New(conn)

	// Test: schema init (idempotent)
	require.NoError(t, s.Init(ctx))
	require.NoError(t, s.Init(ctx)) // second call must also succeed

	// Test: single node becomes leader
	refresh := 150 * time.Second
	isMain, err := s.IsLeader(ctx, refresh)
	require.NoError(t, err)
	require.True(t, isMain, "sole node should be the leader")

	// Test: lease was acquired (lease_expires_at is in the future)
	leaseExpiresAt := getLeaseExpiry(ctx, t, conn)
	require.NotNil(t, leaseExpiresAt)
	require.True(t, leaseExpiresAt.After(time.Now()), "lease should be in the future")

	// Test: lease renewal extends lease_expires_at
	prevLease := *leaseExpiresAt
	time.Sleep(10 * time.Millisecond) // ensure clock advances
	isMain, err = s.IsLeader(ctx, refresh)
	require.NoError(t, err)
	require.True(t, isMain)
	leaseExpiresAt = getLeaseExpiry(ctx, t, conn)
	require.True(t, leaseExpiresAt.After(prevLease), "lease should have been extended")

	// Test: sticky leadership — another node cannot steal a valid lease
	s2 := NewWithNodeID(conn, 999999)
	isMain, err = s2.IsLeader(ctx, refresh)
	require.NoError(t, err)
	require.False(t, isMain, "node 999999 should not steal leadership while current lease is valid")

	// Original leader should still hold the lease
	isMain, err = s.IsLeader(ctx, refresh)
	require.NoError(t, err)
	require.True(t, isMain, "original leader should retain leadership")

	// Test: when current leader's lease expires, a new leader is elected
	expireLease(ctx, t, conn)

	isMain, err = s2.IsLeader(ctx, refresh)
	require.NoError(t, err)
	require.True(t, isMain, "node 999999 should be elected after old leader's lease expires")

	// Expire the lease held by node 999999 so the real node can reclaim
	expireLease(ctx, t, conn)
	deleteNode(ctx, t, conn, 999999)

	// Test: current node becomes leader again as the sole active node
	isMain, err = s.IsLeader(ctx, refresh)
	require.NoError(t, err)
	require.True(t, isMain, "should be leader again as sole active node")

	// Test: two instances with same node ID — only the claimer can renew
	// Expire the current lease so a fresh claim can happen.
	expireLease(ctx, t, conn)

	const sharedNodeID int64 = 888888
	instanceA := NewWithNodeID(conn, sharedNodeID)
	instanceB := NewWithNodeID(conn, sharedNodeID)

	// Instance A claims leadership.
	isMain, err = instanceA.IsLeader(ctx, refresh)
	require.NoError(t, err)
	require.True(t, isMain, "instance A should claim leadership")

	// Instance A can renew.
	isMain, err = instanceA.IsLeader(ctx, refresh)
	require.NoError(t, err)
	require.True(t, isMain, "instance A should renew its lease")

	// Instance B has a different (zero) nonce, so renewal must fail.
	// The lease is still valid, so it should not be able to claim either.
	isMain, err = instanceB.IsLeader(ctx, refresh)
	require.NoError(t, err)
	require.False(t, isMain, "instance B must not be leader — nonce mismatch")

	// Cleanup: expire the lease held by sharedNodeID.
	expireLease(ctx, t, conn)
	deleteNode(ctx, t, conn, sharedNodeID)
}
