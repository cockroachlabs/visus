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

//go:build integration

package collector

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/cockroachlabs/visus/internal/database"
	"github.com/cockroachlabs/visus/internal/testutil"
	"github.com/stretchr/testify/require"
)

// setTestRows replaces the contents of the "test" table with the given
// samples, so the next Collect call observes a fresh snapshot. This is how
// these tests simulate a metric changing over time against a real table,
// in place of pgxmock handing back a different canned result set per call.
func setTestRows(ctx context.Context, t *testing.T, conn database.Connection, rows []sample) {
	t.Helper()
	r := require.New(t)
	_, err := conn.Exec(ctx, "DELETE FROM test")
	r.NoError(err)
	for _, row := range rows {
		_, err := conn.Exec(ctx,
			"INSERT INTO test (label, counter, gauge) VALUES ($1, $2, $3)",
			row.label, row.counter, row.gauge)
		r.NoError(err)
	}
}

func testCollect(t *testing.T, collector Collector, conn database.Connection, rows []sample) {
	t.Helper()
	setTestRows(context.Background(), t, conn, rows)
	err := collector.Collect(context.Background(), conn)
	require.NoError(t, err)
}

func testDatabaseCollect(
	t *testing.T, collector Collector, conn database.Connection, rows []sample,
) {
	t.Helper()
	setTestRows(context.Background(), t, conn, rows)
	err := collector.Collect(context.Background(), conn)
	require.NoError(t, err)
}

func TestCollect(t *testing.T) {
	a, r := assertions(t)
	ctx := context.Background()
	conn, _ := testutil.NewDatabase(ctx, t)
	_, err := conn.Exec(ctx, "CREATE TABLE test (label STRING, counter FLOAT8, gauge FLOAT8)")
	r.NoError(err)

	counter := "counter"
	gauge := "gauge"
	prefix := "collect"
	counterMetricName := strings.Join([]string{prefix, counter}, "_")
	gaugeMetricName := strings.Join([]string{prefix, gauge}, "_")
	coll := newCollector(prefix, []string{"label"}, "",
		"SELECT label, counter, gauge from test limit $1").
		WithMaxResults(maxResults)
	err = coll.AddCounter(counter, counter)
	r.NoError(err)
	err = coll.AddGauge(gauge, gauge)
	r.NoError(err)
	collector := coll.(*collector)
	collector.maybeInitCache()
	r.Equal(4, collector.countersCache.MaxEntries)
	r.Equal(0, collector.countersCache.Len())
	tests := []test{
		{
			"start",
			[]sample{
				{"test1", 1, 1},
				{"test2", 1, 5},
			},
			map[string][]string{
				counterMetricName: {"label:test1 1.000000", "label:test2 1.000000"},
				gaugeMetricName:   {"label:test1 1.000000", "label:test2 5.000000"},
			},
			2,
		},
		{
			"counter_increase",
			[]sample{
				{"test1", 1, 3},
				{"test2", 1, 1},
			},
			map[string][]string{
				counterMetricName: {"label:test1 1.000000", "label:test2 1.000000"},
				gaugeMetricName:   {"label:test1 3.000000", "label:test2 1.000000"},
			},
			2,
		},
		{
			"counter_increase_again",
			[]sample{
				{"test1", 4, 1},
				{"test2", 2, 1},
			},
			map[string][]string{
				counterMetricName: {"label:test1 4.000000", "label:test2 2.000000"},
				gaugeMetricName:   {"label:test1 1.000000", "label:test2 1.000000"},
			},
			2,
		},
		{
			"counter_reset",
			[]sample{
				{"test1", 2, 1},
				{"test3", 2, 1},
			},
			map[string][]string{
				counterMetricName: {"label:test1 6.000000", "label:test2 2.000000", "label:test3 2.000000"},
				gaugeMetricName:   {"label:test1 1.000000", "label:test3 1.000000"},
			},
			3,
		},
		{
			"new_labels",
			[]sample{
				{"test1", 2, 1},
				{"test4", 2, 1},
			},
			map[string][]string{
				counterMetricName: {"label:test1 6.000000", "label:test2 2.000000", "label:test3 2.000000", "label:test4 2.000000"},
				gaugeMetricName:   {"label:test1 1.000000", "label:test4 1.000000"},
			},
			4,
		},
		{
			"test2_evicted",
			[]sample{
				{"test1", 2, 1},
				{"test5", 2, 1},
			},
			map[string][]string{
				counterMetricName: {"label:test1 6.000000", "label:test3 2.000000", "label:test4 2.000000", "label:test5 2.000000"},
				gaugeMetricName:   {"label:test1 1.000000", "label:test5 1.000000"},
			},
			4,
		},
	}
	// run sequentially only
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testCollect(t, collector, conn, tt.samples)
			testVerify(t, prefix, tt.expected)
			expectedGaugeLabels := make([]string, len(tt.samples))
			for i, s := range tt.samples {
				expectedGaugeLabels[i] = s.label
			}
			var actualGaugeLabels []string
			for k := range collector.gaugeLabels[gauge] {
				actualGaugeLabels = append(actualGaugeLabels, k)
			}
			a.ElementsMatch(expectedGaugeLabels,
				actualGaugeLabels)
			a.Equal(tt.cacheLen, collector.countersCache.Len())
		})
	}
}

func TestDatabaseCollect(t *testing.T) {
	a, r := assertions(t)
	ctx := context.Background()
	conn, dbName := testutil.NewDatabase(ctx, t)
	_, err := conn.Exec(ctx, "CREATE TABLE test (label STRING, counter FLOAT8, gauge FLOAT8)")
	r.NoError(err)

	counter := "counter"
	gauge := "gauge"
	prefix := "dbcollect"
	counterMetricName := strings.Join([]string{prefix, counter}, "_")
	gaugeMetricName := strings.Join([]string{prefix, gauge}, "_")
	coll := newCollector("testdb", []string{"label"},
		fmt.Sprintf("SELECT '%s'", dbName),
		"SELECT label, counter, gauge from test limit $1").
		WithMaxResults(maxResults)
	err = coll.AddCounter(counter, counter)
	r.NoError(err)
	err = coll.AddGauge(gauge, gauge)
	r.NoError(err)
	collector := coll.(*collector)
	collector.maybeInitCache()
	r.Equal(4, collector.countersCache.MaxEntries)
	r.Equal(0, collector.countersCache.Len())
	tests := []test{
		{
			"start",
			[]sample{
				{"test1", 1, 1},
				{"test2", 1, 5},
			},
			map[string][]string{
				counterMetricName: {fmt.Sprintf("_database:%s,label:test1 1.000000", dbName), fmt.Sprintf("_database:%s,label:test2 1.000000", dbName)},
				gaugeMetricName:   {fmt.Sprintf("_database:%s,label:test1 1.000000", dbName), fmt.Sprintf("_database:%s,label:test2 5.000000", dbName)},
			},
			2,
		},
		{
			"counter_increase",
			[]sample{
				{"test1", 1, 3},
				{"test2", 1, 1},
			},
			map[string][]string{
				counterMetricName: {fmt.Sprintf("_database:%s,label:test1 1.000000", dbName), fmt.Sprintf("_database:%s,label:test2 1.000000", dbName)},
				gaugeMetricName:   {fmt.Sprintf("_database:%s,label:test1 3.000000", dbName), fmt.Sprintf("_database:%s,label:test2 1.000000", dbName)},
			},
			2,
		},
		{
			"counter_increase_again",
			[]sample{
				{"test1", 4, 1},
				{"test2", 2, 1},
			},
			map[string][]string{
				counterMetricName: {fmt.Sprintf("_database:%s,label:test1 4.000000", dbName), fmt.Sprintf("_database:%s,label:test2 2.000000", dbName)},
				gaugeMetricName:   {fmt.Sprintf("_database:%s,label:test1 1.000000", dbName), fmt.Sprintf("_database:%s,label:test2 1.000000", dbName)},
			},
			2,
		},
		{
			"counter_reset",
			[]sample{
				{"test1", 2, 1},
				{"test3", 2, 1},
			},
			map[string][]string{
				counterMetricName: {fmt.Sprintf("_database:%s,label:test1 6.000000", dbName), fmt.Sprintf("_database:%s,label:test2 2.000000", dbName), fmt.Sprintf("_database:%s,label:test3 2.000000", dbName)},
				gaugeMetricName:   {fmt.Sprintf("_database:%s,label:test1 1.000000", dbName), fmt.Sprintf("_database:%s,label:test3 1.000000", dbName)},
			},
			3,
		},
		{
			"new_labels",
			[]sample{
				{"test1", 2, 1},
				{"test4", 2, 1},
			},
			map[string][]string{
				counterMetricName: {fmt.Sprintf("_database:%s,label:test1 6.000000", dbName), fmt.Sprintf("_database:%s,label:test2 2.000000", dbName), fmt.Sprintf("_database:%s,label:test3 2.000000", dbName), fmt.Sprintf("_database:%s,label:test4 2.000000", dbName)},
				gaugeMetricName:   {fmt.Sprintf("_database:%s,label:test1 1.000000", dbName), fmt.Sprintf("_database:%s,label:test4 1.000000", dbName)},
			},
			4,
		},
		{
			"test2_evicted",
			[]sample{
				{"test1", 2, 1},
				{"test5", 2, 1},
			},
			map[string][]string{
				counterMetricName: {fmt.Sprintf("_database:%s,label:test1 6.000000", dbName), fmt.Sprintf("_database:%s,label:test3 2.000000", dbName), fmt.Sprintf("_database:%s,label:test4 2.000000", dbName), fmt.Sprintf("_database:%s,label:test5 2.000000", dbName)},
				gaugeMetricName:   {fmt.Sprintf("_database:%s,label:test1 1.000000", dbName), fmt.Sprintf("_database:%s,label:test5 1.000000", dbName)},
			},
			4,
		},
	}
	// run sequentially only
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testDatabaseCollect(t, collector, conn, tt.samples)
			testVerify(t, prefix, tt.expected)
			a.Equal(tt.cacheLen, collector.countersCache.Len())
			expectedGaugeLabels := make([]string, len(tt.samples))
			for i, s := range tt.samples {
				expectedGaugeLabels[i] = fmt.Sprintf("%s|%s", s.label, dbName)
			}
			var actualGaugeLabels []string
			for k := range collector.gaugeLabels[gauge] {
				actualGaugeLabels = append(actualGaugeLabels, k)
			}
			a.ElementsMatch(expectedGaugeLabels, actualGaugeLabels)
		})
	}
}
