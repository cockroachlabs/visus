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

package collection

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/cockroachlabs/visus/internal/store"
	"github.com/cockroachlabs/visus/internal/testutil"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

// TestMain starts the shared CockroachDB cluster used by every integration
// test in this package.
func TestMain(m *testing.M) {
	os.Exit(testutil.StartCluster(m))
}

// TestCommandsAgainstRealConnection covers the "test" subcommand path,
// which is the only CLI command that runs a collector query against a
// real connection rather than the in-memory store used by TestCommands.
func TestCommandsAgainstRealConnection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r := require.New(t)

	conn, _ := testutil.NewDatabase(ctx, t)
	_, err := conn.Exec(ctx, "CREATE TABLE stats (database STRING, queries FLOAT8)")
	r.NoError(err)
	_, err = conn.Exec(ctx, "INSERT INTO stats VALUES ('one', 10), ('two', 12)")
	r.NoError(err)

	collection1 := &store.Collection{
		Frequency: pgtype.Interval{Microseconds: 1e6, Valid: true},
		Labels:    []string{"database"},
		MaxResult: 1,
		Metrics: []store.Metric{
			{
				Name: "queries",
				Kind: store.Counter,
				Help: "total queries per database",
			},
		},
		Name:  "collection_01",
		Query: "SELECT database,queries FROM stats LIMIT $1",
	}

	tests := []commandTest{
		{
			args:               []string{"test", "--interval", "1s", "--url", "fake://", collection1.Name},
			expectedOut:        "HELP collection_01_queries total queries per database",
			expectedStoreNames: []string{collection1.Name},
			initialStore:       []*store.Collection{collection1},
			name:               "test collection",
			mock:               conn,
		},
		{
			args:               []string{"test", "--interval", "1s", "--allow-unsafe-internals", "--url", "fake://", collection1.Name},
			expectedOut:        "HELP collection_01_queries total queries per database",
			expectedStoreNames: []string{collection1.Name},
			initialStore:       []*store.Collection{collection1},
			name:               "test collection with allow-unsafe-internals flag",
			mock:               conn,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.execute(ctx, t)
		})
	}
}
