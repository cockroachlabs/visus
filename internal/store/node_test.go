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

package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/cockroachlabs/visus/internal/store"
	"github.com/cockroachlabs/visus/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetNodesEmpty(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	r := require.New(t)
	st, _ := testutil.NewStore(ctx, t)

	nodes, err := st.GetNodes(ctx)
	r.NoError(err)
	assert.Empty(t, nodes)
}

func TestRegisterNodeAndGetNodes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	r := require.New(t)
	st, _ := testutil.NewStore(ctx, t)

	id1, err := st.RegisterNode(ctx, "host-a", 1234, "v1.0.0")
	r.NoError(err)
	id2, err := st.RegisterNode(ctx, "host-b", 5678, "v1.1.0")
	r.NoError(err)
	assert.NotEqual(t, id1, id2)

	nodes, err := st.GetNodes(ctx)
	r.NoError(err)
	r.Len(nodes, 2)

	byHostname := make(map[string]store.NodeInfo, len(nodes))
	for _, n := range nodes {
		byHostname[n.Hostname] = n
	}
	r.Contains(byHostname, "host-a")
	assert.Equal(t, id1, byHostname["host-a"].ID)
	assert.Equal(t, 1234, byHostname["host-a"].PID)
	assert.Equal(t, "v1.0.0", byHostname["host-a"].Version)
	r.Contains(byHostname, "host-b")
	assert.Equal(t, id2, byHostname["host-b"].ID)
	assert.Equal(t, 5678, byHostname["host-b"].PID)
	assert.Equal(t, "v1.1.0", byHostname["host-b"].Version)
}

func TestHeartbeat(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	r := require.New(t)
	st, _ := testutil.NewStore(ctx, t)

	id, err := st.RegisterNode(ctx, "myhost", 42, "v1.0.0")
	r.NoError(err)
	nodes, err := st.GetNodes(ctx)
	r.NoError(err)
	r.Len(nodes, 1)
	before := nodes[0].Updated

	// The heartbeat timestamp has second-level precision, so give it a
	// moment to move forward before comparing.
	time.Sleep(1100 * time.Millisecond)
	r.NoError(st.Heartbeat(ctx, id))

	nodes, err = st.GetNodes(ctx)
	r.NoError(err)
	r.Len(nodes, 1)
	assert.True(t, nodes[0].Updated.After(before), "heartbeat did not advance the updated timestamp")
}

func TestDeleteNode(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	r := require.New(t)
	st, _ := testutil.NewStore(ctx, t)

	id, err := st.RegisterNode(ctx, "myhost", 42, "v1.0.0")
	r.NoError(err)

	r.NoError(st.DeleteNode(ctx, id))

	nodes, err := st.GetNodes(ctx)
	r.NoError(err)
	assert.Empty(t, nodes)
}

// TestNodeStoreErrors verifies that node store methods propagate a real
// connection failure instead of swallowing it. The exact driver error text
// isn't asserted since it comes from pgx/CockroachDB, not from this
// package.
func TestNodeStoreErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	r := require.New(t)
	st, _ := testutil.NewStore(ctx, t)

	canceled, stop := context.WithCancel(ctx)
	stop()

	_, err := st.GetNodes(canceled)
	assert.Error(t, err)

	_, err = st.RegisterNode(canceled, "myhost", 42, "v1.0.0")
	assert.Error(t, err)

	id, err := st.RegisterNode(ctx, "myhost", 42, "v1.0.0")
	r.NoError(err)

	err = st.Heartbeat(canceled, id)
	assert.Error(t, err)

	err = st.DeleteNode(canceled, id)
	assert.Error(t, err)
}
