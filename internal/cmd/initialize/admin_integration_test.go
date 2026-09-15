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

package initialize

import (
	"context"
	"io"
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

// captureStdout runs f with os.Stdout redirected to a pipe and returns
// whatever f wrote to it. admin.go prints its result with fmt.Printf
// directly to os.Stdout rather than through the cobra command's output
// writer, so cmd.SetOut has nothing to capture.
func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	r := require.New(t)

	pipeReader, pipeWriter, err := os.Pipe()
	r.NoError(err)
	orig := os.Stdout
	os.Stdout = pipeWriter
	defer func() { os.Stdout = orig }()

	f()

	r.NoError(pipeWriter.Close())
	out, err := io.ReadAll(pipeReader)
	r.NoError(err)
	return string(out)
}

// TestInitCommandIdempotent verifies that running the "init" CLI command
// twice against the same database succeeds both times, since it's the
// upgrade path an operator runs on every restart.
func TestInitCommandIdempotent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	r := require.New(t)

	url := testutil.PGURL().String()

	for range 2 {
		cmd := Command()
		cmd.SetContext(ctx)
		cmd.SetArgs([]string{"--url", url})
		out := captureStdout(t, func() {
			r.NoError(cmd.Execute())
		})
		r.Contains(out, "Database initialized at")
	}
}
