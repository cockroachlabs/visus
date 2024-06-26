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

package scanner

import (
	"bytes"
	"context"
	_ "embed" // embedding sql statements
	"testing"
	"time"

	"github.com/cockroachlabs/visus/internal/store"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/expfmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testVerify(t *testing.T, expected map[string]string) {
	gathering, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	for _, mf := range gathering {
		toCheck, ok := expected[*mf.Name]
		if ok {
			out := bytes.NewBufferString("")
			expfmt.MetricFamilyToText(out, mf)
			assert.Equal(t, toCheck, out.String())
		}
	}
}

//go:embed testdata/all.txt
var allExpected string

//go:embed testdata/regex.txt
var regexExpected string

//go:embed testdata/max.txt
var maxExpected string

//go:embed testdata/cache.txt
var cacheExpected string

// TestScanner verifies we can produce metrics from a test file.
func TestScanner(t *testing.T) {
	tests := []struct {
		name   string
		target *store.Scan
		want   map[string]string
	}{
		{
			"all",
			&store.Scan{
				Enabled: true,
				Format:  store.CRDBV2,
				Name:    "crdb",
				Path:    "./testdata/sample.log",
				Patterns: []store.Pattern{
					{
						Name:  "all",
						Regex: "",
						Help:  "all events",
					},
				},
			},
			map[string]string{
				"crdb_all": allExpected,
			},
		},
		{
			"regex",
			&store.Scan{
				Enabled: true,
				Format:  store.CRDBV2,
				Name:    "crdb",
				Path:    "./testdata/sample.log",
				Patterns: []store.Pattern{
					{
						Name:  "size",
						Regex: "(cache|max) size",
						Help:  "size events",
					},
				},
			},
			map[string]string{
				"crdb_size": regexExpected,
			},
		},
		{
			"multiple regex",
			&store.Scan{
				Enabled: true,
				Format:  store.CRDBV2,
				Name:    "crdb",
				Path:    "./testdata/sample.log",
				Patterns: []store.Pattern{
					{
						Name:  "cache",
						Regex: "cache size",
						Help:  "cache events",
					},
					{
						Name:  "max",
						Regex: "max size",
						Help:  "max events",
					},
				},
			},
			map[string]string{
				"crdb_max":   maxExpected,
				"crdb_cache": cacheExpected,
			},
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			s, err := FromConfig(tt.target, &Config{
				FromBeginning: true,
			}, prometheus.DefaultRegisterer)
			r.NoError(err)
			tail, err := s.scan()
			r.NoError(err)
			err = s.parser(ctx, tail, s.metrics)
			r.NoError(err)
			testVerify(t, tt.want)
		})
	}
}
