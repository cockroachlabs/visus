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

package scan

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
	"github.com/cockroachlabs/visus/internal/store"
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
	initialStore       []*store.Scan
	name               string
	stdIn              string
}

func (c *commandTest) execute(ctx context.Context, t *testing.T) {
	r := require.New(t)
	a := assert.New(t)
	env := env.Testing(ctx)
	st, err := env.ProvideStore(ctx, "")
	r.NoError(err)
	for _, scan := range c.initialStore {
		r.NoError(st.PutScan(ctx, scan))
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
	names, err := st.GetScanNames(ctx)
	r.NoError(err)
	slices.Sort(c.expectedStoreNames)
	a.Equal(c.expectedStoreNames, names)
}

// TestCommands verifies that the behavior of each CLI command.
func TestCommands(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r := require.New(t)
	scan1 := &store.Scan{
		Name:   "scan_01",
		Path:   "testdata/sample.log",
		Format: "crdb-v2",
		Patterns: []store.Pattern{
			{
				Help: "description",
				Name: "metric",
			},
		},
	}
	mscan1, err := marshal(scan1)
	r.NoError(err)
	scan1Cfg := string(mscan1)
	scan2 := &store.Scan{Name: "scan_02"}
	mscan2, err := marshal(scan2)
	r.NoError(err)
	scan2Cfg := string(mscan2)
	scans := []*store.Scan{scan1, scan2}
	scanNames := []string{"scan_01", "scan_02"}
	tests := []commandTest{
		// Delete
		{
			args:               []string{"delete", "--url", "fake://", scan1.Name},
			expectedOut:        fmt.Sprintf("Scan %s deleted.\n", scan1.Name),
			expectedStoreNames: []string{},
			initialStore:       []*store.Scan{scan1},
			name:               "delete scan",
		},
		{
			args:          []string{"delete", "--url", "fake://", scan1.Name},
			expectedError: "injected error",
			forcedError:   errors.New("injected error"),
			initialStore:  []*store.Scan{scan1},
			name:          "delete scan store error",
		},
		// Get
		{
			args:               []string{"get", "--url", "fake://", scan1.Name},
			expectedOut:        scan1Cfg,
			expectedStoreNames: []string{scan1.Name},
			initialStore:       []*store.Scan{scan1},
			name:               "get scan",
		},
		{
			args:               []string{"get", "--url", "fake://", "not_there"},
			expectedOut:        "not found",
			expectedStoreNames: []string{scan1.Name},
			initialStore:       []*store.Scan{scan1},
			name:               "get not existent scan",
		},
		{
			args:          []string{"get", "--url", "fake://", scan1.Name},
			expectedError: "injected error",
			forcedError:   errors.New("injected error"),
			initialStore:  []*store.Scan{scan1},
			name:          "get scan store error",
		},
		// List
		{
			args:               []string{"list", "--url", "fake://"},
			expectedOut:        strings.Join(scanNames, "\n"),
			expectedStoreNames: scanNames,
			initialStore:       scans,
			name:               "list scan",
		},
		{
			args:          []string{"list", "--url", "fake://", scan1.Name},
			expectedError: "injected error",
			forcedError:   errors.New("injected error"),
			initialStore:  []*store.Scan{scan1},
			name:          "list scan store error",
		},
		// Put
		{
			args:               []string{"put", "--url", "fake://", "--yaml", "-"},
			expectedOut:        fmt.Sprintf("Scan %s inserted.\n", scan2.Name),
			expectedStoreNames: scanNames,
			initialStore:       []*store.Scan{scan1},
			name:               "put scan",
			stdIn:              scan2Cfg,
		},
		{
			args:          []string{"put", "--url", "fake://", "--yaml"},
			expectedError: "flag needs an argument",
			initialStore:  []*store.Scan{scan1},
			name:          "put scan no yaml arg",
			stdIn:         scan2Cfg,
		},
		{
			args:          []string{"put", "--url", "fake://"},
			expectedError: "yaml configuration required",
			initialStore:  []*store.Scan{scan1},
			name:          "put scan no yaml flag",
			stdIn:         scan2Cfg,
		},
		{
			args:          []string{"put", "--url", "fake://", "--yaml", "-"},
			expectedError: "injected error",
			forcedError:   errors.New("injected error"),
			initialStore:  []*store.Scan{scan1},
			name:          "put scan store error",
			stdIn:         scan2Cfg,
		},
		// Test
		{
			args:               []string{"test", "--interval", "1s", "--url", "fake://", scan1.Name},
			expectedOut:        "HELP scan_01_metric description",
			expectedStoreNames: []string{scan1.Name},
			initialStore:       []*store.Scan{scan1},
			name:               "test scan",
		},
		{
			args:               []string{"test", "--url", "fake://", "not_there"},
			expectedOut:        "not found",
			expectedStoreNames: []string{scan1.Name},
			initialStore:       []*store.Scan{scan1},
			name:               "test not existent scan",
		},
		{
			args:          []string{"test", "--url", "fake://", scan1.Name},
			expectedError: "injected error",
			forcedError:   errors.New("injected error"),
			initialStore:  []*store.Scan{scan1},
			name:          "test scan store error",
		},
	}
	for _, tst := range tests {
		t.Run(tst.name, func(t *testing.T) {
			tst.execute(ctx, t)
		})
	}
}
