// Copyright 2022 Cockroach Labs Inc.
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

package collector

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/pashagolub/pgxmock/v3"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testVerify(t *testing.T, expected map[string][]string) {
	gathering, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	for _, mf := range gathering {
		if strings.HasPrefix(*mf.Name, "crdb") {
			results := make([]string, 0)
			for _, m := range mf.GetMetric() {
				switch *mf.Type {
				case dto.MetricType_COUNTER:
					results = append(results, fmt.Sprintf("%s %s %f", m.GetLabel()[0].GetName(), m.GetLabel()[0].GetValue(), m.GetCounter().GetValue()))
				case dto.MetricType_GAUGE:
					results = append(results, fmt.Sprintf("%s %s %f", m.GetLabel()[0].GetName(), m.GetLabel()[0].GetValue(), m.GetGauge().GetValue()))
				}
			}
			assert.Equal(t, expected[*mf.Name], results)
		}
	}
}

type sample struct {
	label   string
	gauge   float64
	counter float64
}
type test struct {
	name     string
	samples  []sample
	expected map[string][]string
	cacheLen int
}

const maxResults = 2

func assertions(t *testing.T) (*assert.Assertions, *require.Assertions) {
	return assert.New(t), require.New(t)
}

func testCollect(t *testing.T, collector Collector, mock pgxmock.PgxConnIface, rows []sample) {
	columns := []string{"label", "gauge", "counter"}
	query := mock.ExpectQuery("SELECT label, counter, gauge from test limit .+").WithArgs(maxResults)
	res := mock.NewRows(columns)
	for _, row := range rows {
		res.AddRow(row.label, row.gauge, row.counter)
	}
	query.WillReturnRows(res)
	err := collector.Collect(context.Background(), mock)
	require.NoError(t, err)
}
func TestCollector_Collect(t *testing.T) {
	a, r := assertions(t)
	mock, err := pgxmock.NewConn()
	r.NoError(err)
	coll := New("test", []string{"label"}, "SELECT label, counter, gauge from test limit $1").
		WithMaxResults(maxResults)
	err = coll.AddCounter("counter", "counter")
	r.NoError(err)
	err = coll.AddGauge("gauge", "gauge")
	r.NoError(err)
	collector := coll.(*collector)
	collector.maybeInitCache()
	r.Equal(8, collector.metricsCache.MaxEntries)
	r.Equal(0, collector.metricsCache.Len())
	tests := []test{
		{
			"one",
			[]sample{
				{"test1", 1, 1},
				{"test2", 1, 5},
			},
			map[string][]string{
				"counter": {"label test1 1.000000", "label test2 1.000000"},
				"gauge":   {"label test1 1.000000", "label test2 5.000000"},
			},
			4,
		},
		{
			"two",
			[]sample{
				{"test1", 1, 3},
				{"test2", 1, 1},
			},
			map[string][]string{
				"counter": {"label test1 1.000000", "label test2 1.000000"},
				"gauge":   {"label test1 3.000000", "label test2 1.000000"},
			},
			4,
		},
		{
			"three",
			[]sample{
				{"test1", 4, 1},
				{"test2", 2, 1},
			},
			map[string][]string{
				"counter": {"label test1 4.000000", "label test2 2.000000"},
				"gauge":   {"label test1 1.000000", "label test2 1.000000"},
			},
			4,
		},
		{
			"four_counter_reset",
			[]sample{
				{"test1", 2, 1},
				{"test3", 2, 1},
			},
			map[string][]string{
				"counter": {"label test1 6.000000", "label test2 2.000000", "label test3 2.000000"},
				"gauge":   {"label test1 1.000000", "label test2 1.000000", "label test3 1.000000"},
			},
			6,
		},
		{
			"five",
			[]sample{
				{"test1", 2, 1},
				{"test4", 2, 1},
			},
			map[string][]string{
				"counter": {"label test1 6.000000", "label test2 2.000000", "label test3 2.000000", "label test4 2.000000"},
				"gauge":   {"label test1 1.000000", "label test2 1.000000", "label test3 1.000000", "label test4 1.000000"},
			},
			8,
		},
		{
			"six_test2_evicted",
			[]sample{
				{"test1", 2, 1},
				{"test5", 2, 1},
			},
			map[string][]string{
				"counter": {"label test1 6.000000", "label test3 2.000000", "label test4 2.000000", "label test5 2.000000"},
				"gauge":   {"label test1 1.000000", "label test3 1.000000", "label test4 1.000000", "label test5 1.000000"},
			},
			8,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testCollect(t, collector, mock, tt.samples)
			testVerify(t, tt.expected)
			a.Equal(tt.cacheLen, collector.metricsCache.Len())
		})
	}
}

func Test_collector_AddCounter(t *testing.T) {
	a, r := assertions(t)
	collName := "test"
	coll := New(collName, []string{"label"}, "SELECT label, counter, gauge from test limit $1").(*collector)
	registry := prometheus.NewRegistry()
	coll.registerer = registry

	name := "counter"
	help := "help counter"
	err := coll.AddCounter(name, help)
	r.NoError(err)
	metric, ok := coll.metrics[name]
	a.Equal(true, ok)
	a.Equal(name, metric.name)
	a.Equal(help, metric.help)
	a.Equal(Counter, metric.kind)

	counter, ok := metric.vec.(*prometheus.CounterVec)
	r.Equal(true, ok)
	counter.WithLabelValues("v1").Inc()

	families, err := registry.Gather()
	r.NoError(err)
	r.Equal(1, len(families))
	fam := families[0]
	r.Equal(collName+"_"+name, *fam.Name)
	metrics := fam.Metric
	r.Equal(1, len(metrics))

	another := "another"
	help = "another help"
	err = coll.AddCounter(another, help)
	r.NoError(err)
	metric, ok = coll.metrics[another]
	a.Equal(true, ok)
	a.Equal(another, metric.name)
	a.Equal(help, metric.help)
	a.Equal(Counter, metric.kind)
	r.Equal(2, len(coll.metrics))

	counter, ok = metric.vec.(*prometheus.CounterVec)
	r.Equal(true, ok)
	counter.WithLabelValues("v1").Inc()

	families, err = registry.Gather()
	r.NoError(err)
	r.Equal(2, len(families))

	// changing help is not allowed
	help = "help counter changed"
	err = coll.AddCounter(name, help)
	a.Error(err)
}

func Test_collector_AddGauge(t *testing.T) {
	a, r := assertions(t)
	collName := "test"
	coll := New(collName, []string{"label"}, "SELECT label, counter, gauge from test limit $1").(*collector)
	registry := prometheus.NewRegistry()
	coll.registerer = registry

	name := "gauge"
	help := "help gauge"
	err := coll.AddGauge(name, help)
	r.NoError(err)
	metric, ok := coll.metrics[name]
	a.Equal(true, ok)
	a.Equal(name, metric.name)
	a.Equal(help, metric.help)
	a.Equal(Gauge, metric.kind)

	counter, ok := metric.vec.(*prometheus.GaugeVec)
	r.Equal(true, ok)
	counter.WithLabelValues("v1").Inc()

	families, err := registry.Gather()
	r.NoError(err)
	r.Equal(1, len(families))
	fam := families[0]
	r.Equal(collName+"_"+name, *fam.Name)
	metrics := fam.Metric
	r.Equal(1, len(metrics))

	another := "another"
	help = "another help"
	err = coll.AddGauge(another, help)
	r.NoError(err)
	metric, ok = coll.metrics[another]
	a.Equal(true, ok)
	a.Equal(another, metric.name)
	a.Equal(help, metric.help)
	a.Equal(Gauge, metric.kind)
	r.Equal(2, len(coll.metrics))

	counter, ok = metric.vec.(*prometheus.GaugeVec)
	r.Equal(true, ok)
	counter.WithLabelValues("v1").Inc()

	families, err = registry.Gather()
	r.NoError(err)
	r.Equal(2, len(families))

	// changing help is not allowed
	help = "help counter changed"
	err = coll.AddGauge(name, help)
	a.Error(err)
}
