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

	"github.com/cockroachlabs/visus/internal/store"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testFormatLabels(labels []*dto.LabelPair) string {
	b := strings.Builder{}
	for idx, label := range labels {
		if idx > 0 {
			b.WriteString(",")
		}
		b.WriteString(*label.Name)
		b.WriteString(":")
		b.WriteString(*label.Value)
	}
	return b.String()
}

func testVerify(t *testing.T, prefix string, expected map[string][]string) {
	gathering, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	for _, mf := range gathering {
		if strings.HasPrefix(*mf.Name, prefix) {
			results := make([]string, 0)
			for _, m := range mf.GetMetric() {
				switch *mf.Type {
				case dto.MetricType_COUNTER:
					results = append(results, fmt.Sprintf("%s %f", testFormatLabels(m.GetLabel()), m.GetCounter().GetValue()))
				case dto.MetricType_GAUGE:
					results = append(results, fmt.Sprintf("%s %f", testFormatLabels(m.GetLabel()), m.GetGauge().GetValue()))
				}
			}
			assert.Equal(t, expected[*mf.Name], results)
		}
	}
}

type sample struct {
	label   string
	counter float64
	gauge   float64
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

// newCollector creates a collector with the given name, for testing purposes.
// The labels define the various attributes of the metrics being captured.
// The query is the SQL query being executed to retrieve the metric values. The query must have an argument
// to specify the limit on the number results to be returned. The columns must contain the labels specified.
// The format of the query:
// (SELECT label1,label2, ..., metric1,metric2,... FROM ... WHERE ... LIMIT $1)
func newCollector(name string, labels []string, databases string, query string) *collector {
	labelMap := make(map[string]int)
	for i, l := range labels {
		labelMap[l] = i
	}
	return &collector{
		enabled:    true,
		frequency:  10,
		labelMap:   labelMap,
		labels:     labels,
		maxResults: 100,
		metrics:    make(map[string]metric),
		name:       name,
		databases:  databases,
		query:      query,
		registerer: prometheus.DefaultRegisterer,
	}
}

func TestAddCounter(t *testing.T) {
	a, r := assertions(t)
	collName := "counter"
	coll := newCollector(collName, []string{"label"}, "",
		"SELECT label, counter, gauge from test limit $1")
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

func TestAddGauge(t *testing.T) {
	a, r := assertions(t)
	collName := "gauge"
	coll := newCollector(collName, []string{"label"}, "",
		"SELECT label, counter, gauge from test limit $1")
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

func TestGaugeLifeCycle(t *testing.T) {
	a, r := assertions(t)
	collName := "lifecycle"
	coll := newCollector(collName, []string{"label"}, "",
		"SELECT label, counter, gauge from test limit $1")
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

	coll.Unregister()
	families, err = registry.Gather()
	r.NoError(err)
	r.Equal(0, len(families))

}

// TestCollectSkipsConcurrent verifies that Collect returns immediately,
// without touching the connection, when a collection is already in
// progress.
func TestCollectSkipsConcurrent(t *testing.T) {
	_, r := assertions(t)
	coll := newCollector("concurrent", []string{"label"}, "",
		"SELECT label, counter, gauge from test limit $1").
		WithMaxResults(maxResults)
	c := coll.(*collector)

	// Hold the lock to simulate a concurrent collection in progress.
	c.mu.Lock()

	// Collect should return nil immediately without touching the
	// connection, so a nil connection is safe to pass here.
	err := c.Collect(context.Background(), nil)
	r.NoError(err)

	c.mu.Unlock()
}

// TestFromCollectionUnsupportedKind verifies that FromCollection fails
// instead of silently dropping a metric whose Kind isn't gauge or counter.
func TestFromCollectionUnsupportedKind(t *testing.T) {
	_, r := assertions(t)
	coll := &store.Collection{
		Name: "bad_kind",
		Metrics: []store.Metric{
			{
				Name: "metric",
				Kind: store.Kind("bogus"),
				Help: "help",
			},
		},
	}
	_, err := FromCollection(coll, prometheus.NewRegistry())
	r.Error(err)
}

func TestCounterLifeCycle(t *testing.T) {
	a, r := assertions(t)
	collName := "lifecycle"
	coll := newCollector(collName, []string{"label"}, "",
		"SELECT label, counter, gauge from test limit $1")
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

	coll.Unregister()
	families, err = registry.Gather()
	r.NoError(err)
	r.Equal(0, len(families))
}
