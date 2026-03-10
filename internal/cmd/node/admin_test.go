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

package node

import (
	"context"
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cockroachlabs/visus/internal/cmd/env"
	"github.com/cockroachlabs/visus/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Set this to true to rewrite the golden-output files.
const rewriteFiles = false

// TestRewriteShouldBeFalse ensures that we can't merge this test if rewrite is true.
func TestRewriteShouldBeFalse(t *testing.T) {
	require.False(t, rewriteFiles)
}

// TestHelp ensures that the CLI command can be constructed and
// that all flag binding works.
func TestHelp(t *testing.T) {
	r := require.New(t)
	r.NoError(Command().Help())
}

type commandTest struct {
	args          []string
	expectedError string
	forcedError   error
	goldenFile    string
	name          string
	setup         func(context.Context, *require.Assertions, store.Store)
}

func (c *commandTest) execute(ctx context.Context, t *testing.T) {
	a := assert.New(t)
	r := require.New(t)
	t.Setenv("TZ", "UTC")
	var callCount atomic.Int64
	baseTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	timeFn := func() time.Time {
		n := callCount.Add(1) - 1
		return baseTime.Add(time.Duration(n) * time.Second)
	}
	env := env.TestingWithTime(ctx, timeFn)
	if c.setup != nil {
		st, err := env.ProvideStore(ctx, "")
		r.NoError(err)
		c.setup(ctx, r, st)
	}
	if c.forcedError != nil {
		env.InjectStoreError(c.forcedError)
	}
	out, err := env.TestCommand(command, c.args)
	if c.expectedError != "" {
		r.ErrorContains(err, c.expectedError)
		return
	}
	r.NoError(err)
	if rewriteFiles {
		r.NoError(os.WriteFile(c.goldenFile, []byte(out), 0644))
		return
	}
	golden, err := os.ReadFile(c.goldenFile)
	r.NoError(err)
	a.Equal(string(golden), out)
}

// TestCommands verifies the behavior of each CLI command.
func TestCommands(t *testing.T) {
	// 5 seconds for all the tests.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tests := []commandTest{
		{
			args:       []string{"list", "--url", "fake://"},
			goldenFile: "testdata/list_empty.txt",
			name:       "list nodes empty",
		},
		{
			args:       []string{"list", "--url", "fake://"},
			goldenFile: "testdata/list_nodes.txt",
			name:       "list nodes",
			setup: func(ctx context.Context, r *require.Assertions, st store.Store) {
				_, err := st.RegisterNode(ctx, "host-1", 1001, "v1.0")
				r.NoError(err)
				_, err = st.RegisterNode(ctx, "host-2", 2002, "v2.0")
				r.NoError(err)
			},
		},
		{
			args:          []string{"list", "--url", "fake://"},
			expectedError: "injected error",
			forcedError:   errors.New("injected error"),
			name:          "list nodes error",
		},
	}
	for _, tst := range tests {
		t.Run(tst.name, func(t *testing.T) {
			tst.execute(ctx, t)
		})
	}
}
