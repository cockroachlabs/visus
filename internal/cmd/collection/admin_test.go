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

package collection

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cockroachlabs/visus/internal/cmd/env"
	"github.com/cockroachlabs/visus/internal/database"
	"github.com/cockroachlabs/visus/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/pashagolub/pgxmock/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHelp ensures that the CLI command can be constructed and
// that all flag binding works.
func TestHelp(t *testing.T) {
	r := require.New(t)
	r.NoError(Command().Help())
}

type commandTest struct {
	args               []string
	expectedError      string
	expectedOut        string
	expectedStoreNames []string
	forcedError        error
	initialStore       []*store.Collection
	mock               database.Connection
	name               string
	stdIn              string
}

func (c *commandTest) execute(ctx context.Context, t *testing.T) {
	r := require.New(t)
	a := assert.New(t)
	env := env.Testing(ctx)
	if c.mock != nil {
		env.InjectMockConnection(c.mock)
	}
	st, err := env.ProvideStore(ctx, "")
	r.NoError(err)
	for _, collection := range c.initialStore {
		r.NoError(st.PutCollection(ctx, collection))
	}
	if c.forcedError != nil {
		env.InjectStoreError(c.forcedError)
	}
	if c.stdIn != "" {
		env.InjectReader(bytes.NewReader([]byte(c.stdIn)))
	}
	out, err := env.TestCommand(command, c.args)
	if c.expectedError != "" {
		a.ErrorContains(err, c.expectedError)
		return
	}
	a.Contains(out, c.expectedOut)
	names, err := st.GetCollectionNames(ctx)
	r.NoError(err)
	slices.Sort(c.expectedStoreNames)
	a.Equal(c.expectedStoreNames, names)
}

func mockResults(t *testing.T) database.Connection {
	r := require.New(t)
	mock, err := pgxmock.NewConn()
	r.NoError(err)
	columns := []string{"database", "queries"}
	query := mock.ExpectQuery("SELECT database,queries FROM stats LIMIT .+").WithArgs(1)
	res := mock.NewRows(columns)
	res.AddRow("one", 10)
	res.AddRow("two", 12)
	query.WillReturnRows(res)
	return mock
}

// TestCommands verifies that the behavior of each CLI command.
func TestCommands(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r := require.New(t)
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
	mcollection1, err := marshal(collection1)
	r.NoError(err)
	collection1Cfg := string(mcollection1)
	collection2 := &store.Collection{
		Frequency: pgtype.Interval{Microseconds: 1e6, Valid: true},
		Labels:    []string{"database"},
		MaxResult: 1,
		Metrics: []store.Metric{
			{
				Name: "tables",
				Kind: store.Counter,
				Help: "total tables per database",
			},
		},
		Name:  "collection_02",
		Query: "select database,tables from stats limit $1",
	}
	mcollection2, err := marshal(collection2)
	r.NoError(err)
	collection2Cfg := string(mcollection2)
	collections := []*store.Collection{collection1, collection2}
	collectionNames := []string{"collection_01", "collection_02"}
	tests := []commandTest{
		// Delete
		{
			args:               []string{"delete", "--url", "fake://", collection1.Name},
			expectedOut:        fmt.Sprintf("Collection %s deleted.\n", collection1.Name),
			expectedStoreNames: []string{},
			initialStore:       []*store.Collection{collection1},
			name:               "delete collection",
		},
		{
			args:          []string{"delete", "--url", "fake://", collection1.Name},
			expectedError: "injected error",
			forcedError:   errors.New("injected error"),
			initialStore:  []*store.Collection{collection1},
			name:          "delete collection store error",
		},
		// Get
		{
			args:               []string{"get", "--url", "fake://", collection1.Name},
			expectedOut:        collection1Cfg,
			expectedStoreNames: []string{collection1.Name},
			initialStore:       []*store.Collection{collection1},
			name:               "get collection",
		},
		{
			args:               []string{"get", "--url", "fake://", "not_there"},
			expectedOut:        "not found",
			expectedStoreNames: []string{collection1.Name},
			initialStore:       []*store.Collection{collection1},
			name:               "get not existent collection",
		},
		{
			args:          []string{"get", "--url", "fake://", collection1.Name},
			expectedError: "injected error",
			forcedError:   errors.New("injected error"),
			initialStore:  []*store.Collection{collection1},
			name:          "get collection store error",
		},
		// List
		{
			args:               []string{"list", "--url", "fake://"},
			expectedOut:        strings.Join(collectionNames, "\n"),
			expectedStoreNames: collectionNames,
			initialStore:       collections,
			name:               "list collection",
		},
		{
			args:          []string{"list", "--url", "fake://", collection1.Name},
			expectedError: "injected error",
			forcedError:   errors.New("injected error"),
			initialStore:  []*store.Collection{collection1},
			name:          "list collection store error",
		},
		// Put
		{
			args:               []string{"put", "--url", "fake://", "--yaml", "-"},
			expectedOut:        fmt.Sprintf("Collection %s inserted.\n", collection2.Name),
			expectedStoreNames: collectionNames,
			initialStore:       []*store.Collection{collection1},
			name:               "put collection",
			stdIn:              collection2Cfg,
		},
		{
			args:          []string{"put", "--url", "fake://", "--yaml"},
			expectedError: "flag needs an argument",
			initialStore:  []*store.Collection{collection1},
			name:          "put collection no yaml arg",
			stdIn:         collection2Cfg,
		},
		{
			args:          []string{"put", "--url", "fake://"},
			expectedError: "yaml configuration required",
			initialStore:  []*store.Collection{collection1},
			name:          "put collection no yaml flag",
			stdIn:         collection2Cfg,
		},
		{
			args:          []string{"put", "--url", "fake://", "--yaml", "-"},
			expectedError: "injected error",
			forcedError:   errors.New("injected error"),
			initialStore:  []*store.Collection{collection1},
			name:          "put collection store error",
			stdIn:         collection2Cfg,
		},
		// Test
		{
			args:               []string{"test", "--interval", "1s", "--url", "fake://", collection1.Name},
			expectedOut:        "HELP collection_01_queries total queries per database",
			expectedStoreNames: []string{collection1.Name},
			initialStore:       []*store.Collection{collection1},
			name:               "test collection",
			mock:               mockResults(t),
		},
		{
			args:               []string{"test", "--url", "fake://", "not_there"},
			expectedOut:        "not found",
			expectedStoreNames: []string{collection1.Name},
			initialStore:       []*store.Collection{collection1},
			name:               "test not existent collection",
		},
		{
			args:          []string{"test", "--url", "fake://", collection1.Name},
			expectedError: "injected error",
			forcedError:   errors.New("injected error"),
			initialStore:  []*store.Collection{collection1},
			name:          "test collection store error",
		},
	}
	for _, tst := range tests {
		t.Run(tst.name, func(t *testing.T) {
			tst.execute(ctx, t)
		})
	}
}
