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

package histogram

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	initialStore       []*store.Histogram
	name               string
	stdIn              string
}

func (c *commandTest) execute(ctx context.Context, t *testing.T) {
	r := require.New(t)
	a := assert.New(t)
	env := env.Testing(ctx)
	st, err := env.ProvideStore(ctx, "")
	r.NoError(err)
	for _, histogram := range c.initialStore {
		r.NoError(st.PutHistogram(ctx, histogram))
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
	names, err := st.GetHistogramNames(ctx)
	r.NoError(err)
	slices.Sort(c.expectedStoreNames)
	a.Equal(c.expectedStoreNames, names)
}

// TestCommands verifies that the behavior of each CLI command.
func TestCommands(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r := require.New(t)
	ex, err := os.Getwd()
	r.NoError(err)
	testdata := filepath.Clean(filepath.Join(filepath.Dir(ex), "histogram/testdata/sql_latency.txt"))
	histogram1 := &store.Histogram{
		Enabled: true,
		Bins:    10,
		Name:    "histogram_01",
		Regex:   "^sql_exec_latency$",
		Start:   1000000,
		End:     20000000000,
	}
	mhistogram1, err := marshal(histogram1)
	r.NoError(err)
	histogram1Cfg := string(mhistogram1)
	histogram2 := &store.Histogram{
		Enabled: true,
		Bins:    10,
		Name:    "histogram_02",
		Regex:   "^sql_exec_latency$",
		Start:   1000000,
		End:     20000000000,
	}
	mhistogram2, err := marshal(histogram2)
	r.NoError(err)
	histogram2Cfg := string(mhistogram2)
	histograms := []*store.Histogram{histogram1, histogram2}
	histogramNames := []string{"histogram_01", "histogram_02"}
	tests := []commandTest{
		// Delete
		{
			args:               []string{"delete", "--url", "fake://", histogram1.Name},
			expectedOut:        fmt.Sprintf("Histogram %s deleted.\n", histogram1.Name),
			expectedStoreNames: []string{},
			initialStore:       []*store.Histogram{histogram1},
			name:               "delete histogram",
		},
		{
			args:          []string{"delete", "--url", "fake://", histogram1.Name},
			expectedError: "injected error",
			forcedError:   errors.New("injected error"),
			initialStore:  []*store.Histogram{histogram1},
			name:          "delete histogram store error",
		},
		// Get
		{
			args:               []string{"get", "--url", "fake://", histogram1.Name},
			expectedOut:        histogram1Cfg,
			expectedStoreNames: []string{histogram1.Name},
			initialStore:       []*store.Histogram{histogram1},
			name:               "get histogram",
		},
		{
			args:               []string{"get", "--url", "fake://", "not_there"},
			expectedOut:        "not found",
			expectedStoreNames: []string{histogram1.Name},
			initialStore:       []*store.Histogram{histogram1},
			name:               "get not existent histogram",
		},
		{
			args:          []string{"get", "--url", "fake://", histogram1.Name},
			expectedError: "injected error",
			forcedError:   errors.New("injected error"),
			initialStore:  []*store.Histogram{histogram1},
			name:          "get histogram store error",
		},
		// List
		{
			args:               []string{"list", "--url", "fake://"},
			expectedOut:        strings.Join(histogramNames, "\n"),
			expectedStoreNames: histogramNames,
			initialStore:       histograms,
			name:               "list histogram",
		},
		{
			args:          []string{"list", "--url", "fake://", histogram1.Name},
			expectedError: "injected error",
			forcedError:   errors.New("injected error"),
			initialStore:  []*store.Histogram{histogram1},
			name:          "list histogram store error",
		},
		// Put
		{
			args:               []string{"put", "--url", "fake://", "--yaml", "-"},
			expectedOut:        fmt.Sprintf("Histogram %s inserted.\n", histogram2.Name),
			expectedStoreNames: histogramNames,
			initialStore:       []*store.Histogram{histogram1},
			name:               "put histogram",
			stdIn:              histogram2Cfg,
		},
		{
			args:          []string{"put", "--url", "fake://", "--yaml"},
			expectedError: "flag needs an argument",
			initialStore:  []*store.Histogram{histogram1},
			name:          "put histogram no yaml arg",
			stdIn:         histogram2Cfg,
		},
		{
			args:          []string{"put", "--url", "fake://"},
			expectedError: "yaml configuration required",
			initialStore:  []*store.Histogram{histogram1},
			name:          "put histogram no yaml flag",
			stdIn:         histogram2Cfg,
		},
		{
			args:          []string{"put", "--url", "fake://", "--yaml", "-"},
			expectedError: "injected error",
			forcedError:   errors.New("injected error"),
			initialStore:  []*store.Histogram{histogram1},
			name:          "put histogram store error",
			stdIn:         histogram2Cfg,
		},
		// Test
		{
			args:               []string{"test", "--url", "fake://", "--prometheus", "file:///" + testdata},
			expectedOut:        "HELP sql_exec_latency Latency of SQL statement execution",
			expectedStoreNames: []string{histogram1.Name},
			initialStore:       []*store.Histogram{histogram1},
			name:               "test histogram",
		},
		{
			args:          []string{"test", "--url", "fake://", histogram1.Name},
			expectedError: "injected error",
			forcedError:   errors.New("injected error"),
			initialStore:  []*store.Histogram{histogram1},
			name:          "test histogram store error",
		},
	}
	for _, tst := range tests {
		t.Run(tst.name, func(t *testing.T) {
			tst.execute(ctx, t)
		})
	}
}
