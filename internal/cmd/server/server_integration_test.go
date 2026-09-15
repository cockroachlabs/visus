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

package server

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/cockroachlabs/visus/internal/testutil"
	"github.com/stretchr/testify/require"
)

// TestMain starts the shared CockroachDB cluster used by every integration
// test in this package.
func TestMain(m *testing.M) {
	os.Exit(testutil.StartCluster(m))
}

// TestServerNodeLifecycle verifies that RunE wires the node
// registration/heartbeat/deregistration calls correctly: the node appears
// in the store once the server starts, and disappears once the server's
// context is canceled. The store methods themselves (RegisterNode,
// Heartbeat, DeleteNode) are already covered by internal/store/node_test.go;
// this test only exercises server.go's use of them.
func TestServerNodeLifecycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	r := require.New(t)

	// Command()'s RunE does not initialize the schema itself; it expects
	// "visus init" to have already run. testutil.NewStore does that for us
	// and also gives us a Store we can poll from outside the server.
	st, _ := testutil.NewStore(ctx, t)

	runCtx, stopServer := context.WithCancel(ctx)
	defer stopServer()

	cmd := Command()
	cmd.SetContext(runCtx)
	cmd.SetArgs([]string{
		"--insecure",
		"--bind-addr", "127.0.0.1:0",
		"--endpoint", "/test/node-lifecycle",
		"--url", testutil.PGURL().String(),
	})

	done := make(chan error, 1)
	go func() { done <- cmd.Execute() }()

	r.Eventually(func() bool {
		nodes, err := st.GetNodes(ctx)
		return err == nil && len(nodes) == 1
	}, 15*time.Second, 200*time.Millisecond, "server did not register its node")

	stopServer()

	r.Eventually(func() bool {
		nodes, err := st.GetNodes(ctx)
		return err == nil && len(nodes) == 0
	}, 15*time.Second, 200*time.Millisecond, "server did not deregister its node on shutdown")

	select {
	case err := <-done:
		r.NoError(err)
	case <-time.After(15 * time.Second):
		t.Fatal("Command().Execute() did not return after shutdown")
	}
}
