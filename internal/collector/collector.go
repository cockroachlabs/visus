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

// Package collector manages a metric collection.
package collector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/cockroachlabs/visus/internal/database"
	"github.com/cockroachlabs/visus/internal/store"
	"github.com/golang/groupcache/lru"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"
)

// A Collector retrieves all the metric values for all the gauges and counters configured.
type Collector interface {
	fmt.Stringer
	// AddCounter adds a counter to the list of metrics to return.
	AddCounter(name string, help string) error
	// AddGauge adds a gauge to the list of metrics to return.
	AddGauge(name string, help string) error
	// Collect retrieves the values for all the metrics defined.
	Collect(ctx context.Context, conn database.Connection) error
	// GetFrequency returns how often the collector is called, in seconds.
	GetFrequency() int
	// GetLastModified retrieve the last time the job was modified.
	GetLastModified() time.Time
	// Unregister all the prometheus collectors.
	Unregister()
	// WithFrequency sets the frequency
	WithFrequency(frequency int) Collector
	// WithMaxResults sets the maximum number of results to collect each time Collect is called.
	WithMaxResults(max int) Collector
}

// collector implementation. To reduce the cardinality of the metrics, it keeps a cache
// of metrics
type collector struct {
	name         string
	lastModified time.Time
	cardinality  int
	enabled      bool
	frequency    int
	labels       []string
	labelMap     map[string]int
	metrics      map[string]metric
	query        string
	first        bool
	metricsCache *lru.Cache
	maxResults   int
	registerer   prometheus.Registerer
	mu           struct {
		sync.Mutex
		inUse bool
	}
}

// MetricKind specify the type of the metric: counter or gauge.
//
//go:generate go run golang.org/x/tools/cmd/stringer -type=MetricKind
type MetricKind int

const (
	// Undefined is for metrics not currently supported.
	Undefined MetricKind = iota
	// Counter is a cumulative metric that represents a single monotonically increasing value.
	Counter
	// Gauge is a metric that represents a single numerical value that can arbitrarily go up and down.
	Gauge
)

type metric struct {
	help string
	kind MetricKind
	name string
	vec  prometheus.Collector
}

type cacheGC interface {
	DeleteLabelValues(lvs ...string) bool
}
type cacheKey struct {
	Family string
	Labels []string
}

type cacheValue struct {
	labels []string
	vec    cacheGC
	value  float64
}

// New creates a collector with the given name.
// The labels define the various attributes of the metrics being captured.
// The query is the SQL query being executed to retrieve the metric values. The query must have an argument
// to specify the limit on the number results to be returned. The columns must contain the labels specified.
// The format of the query:
// (SELECT label1,label2, ..., metric1,metric2,... FROM ... WHERE ... LIMIT $1)
func New(name string, labels []string, query string) Collector {
	labelMap := make(map[string]int)
	for i, l := range labels {
		labelMap[l] = i
	}
	return &collector{
		enabled:    true,
		name:       name,
		labels:     labels,
		labelMap:   labelMap,
		metrics:    make(map[string]metric),
		query:      query,
		first:      true,
		frequency:  10,
		maxResults: 100,
		registerer: prometheus.DefaultRegisterer,
	}
}

// FromCollection creates a collector from a collection configuration stored in the database.
func FromCollection(
	coll *store.Collection, conn database.Connection, registerer prometheus.Registerer,
) (Collector, error) {
	labelMap := make(map[string]int)
	for i, l := range coll.Labels {
		labelMap[l] = i
	}
	res := &collector{
		enabled:      true,
		name:         coll.Name,
		labels:       coll.Labels,
		labelMap:     labelMap,
		lastModified: coll.LastModified.Time,
		metrics:      make(map[string]metric),
		query:        coll.Query,
		first:        true,
		frequency:    int(coll.Frequency.Microseconds / (1000 * 1000)),
		maxResults:   coll.MaxResult,
		registerer:   registerer,
	}
	for _, m := range coll.Metrics {
		var err error
		switch m.Kind {
		case store.Gauge:
			err = res.AddGauge(m.Name, m.Help)
		case store.Counter:
			err = res.AddCounter(m.Name, m.Help)
		default:
			log.Errorf("%s malformed", coll.Name)
		}
		if err != nil {
			return nil, err
		}
	}
	return res, nil
}

// AddCounter adds a metric counter. The name must match one of the columns returned by the query.
func (c *collector) AddCounter(name string, help string) error {
	c.cardinality++
	metricName := c.name + "_" + name
	vec := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: metricName,
			Help: help,
		},
		c.labels,
	)
	c.registerer.Unregister(vec)
	if err := c.registerer.Register(vec); err != nil {
		return err
	}
	log.Tracef("registering counter %s", metricName)
	c.metrics[name] =
		metric{
			help: help,
			kind: Counter,
			name: name,
			vec:  vec,
		}
	return nil
}

// AddGauge adds a metric gauge. The name must match one of the columns returned by the query.
func (c *collector) AddGauge(name string, help string) error {
	c.cardinality++
	metricName := c.name + "_" + name
	vec := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: metricName,
			Help: help,
		},
		c.labels,
	)
	c.registerer.Unregister(vec)
	if err := c.registerer.Register(vec); err != nil {
		log.Errorf("Unable to register %s. Error = %s", name, err.Error())
		return err
	}
	log.Tracef("registering gauge %s", metricName)
	c.metrics[name] =
		metric{
			help: help,
			kind: Gauge,
			name: name,
			vec:  vec,
		}
	return nil
}

// Collect execute the query and updates the Prometheus metrics.
// (TODO: silvano) handle global collectors (run only on one node)
func (c *collector) Collect(ctx context.Context, conn database.Connection) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.mu.inUse || !c.enabled {
		return nil
	}
	c.mu.inUse = true
	defer func() {
		c.mu.inUse = false
	}()
	c.maybeInitCache()

	query := c.query
	log.Debugf("Collect %s ", c.name)
	log.Tracef("Collect %s query %s ", c.name, query)
	rows, err := conn.Query(ctx, query, c.maxResults)
	if err != nil {
		log.Errorf("Collect %s \n%s", err.Error(), c.query)
		return err
	}
	defer rows.Close()
	c.resetGauges()
	desc := rows.FieldDescriptions()
	if len(desc) != len(c.labels)+len(c.metrics) {
		log.Errorf("%s: column mismatch %v %d %v \n", c.name, desc, len(c.labels), c.metrics)
		return errors.New("columns returned in the query must be match labels+metrics")
	}
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			log.Errorf("%s: unable to decode values; %s", c.name, err.Error())
			continue
		}
		labels := make([]string, len(c.labels))
		for i, v := range values {
			colName := string(desc[i].Name)
			if pos, ok := c.labelMap[colName]; ok {
				if value, ok := v.(string); ok {
					labels[pos] = value
				} else {
					log.Errorf("%s: unknown type %T for label %s", c.name, v, colName)
				}
			} else if _, ok := c.metrics[colName]; ok {
				metric := c.metrics[colName]
				switch vec := metric.vec.(type) {
				case *prometheus.CounterVec:
					switch value := v.(type) {
					case float64:
						c.counterAdd(vec, metric.name, labels, value)
					case int:
						c.counterAdd(vec, metric.name, labels, float64(value))
					case pgtype.Numeric:
						if floatValue, err := value.Float64Value(); err != nil {
							log.Errorf("%s: collect %s", c.name, err.Error())
						} else {
							c.counterAdd(vec, metric.name, labels, floatValue.Float64)
						}
					case nil:
					default:
						log.Errorf("%s: unknown type %T for %s", c.name, v, metric.name)
					}
				case *prometheus.GaugeVec:
					switch value := v.(type) {
					case float64:
						c.gaugeSet(vec, metric.name, labels, value)
					case int:
						c.gaugeSet(vec, metric.name, labels, float64(value))
					case pgtype.Numeric:
						if floatValue, err := value.Float64Value(); err != nil {
							log.Errorf("%s: collect %s", c.name, err.Error())
						} else {
							c.gaugeSet(vec, metric.name, labels, floatValue.Float64)
						}
					case nil:
					default:
						log.Errorf("%s: unknown type %T for %s", c.name, v, metric.name)
					}
				}
			} else {
				log.Errorf("%s: unknown column %s", c.name, colName)
			}
		}
	}
	return nil
}

// GetFrequency returns how often the collector runs (in seconds)
func (c *collector) GetFrequency() int {
	return c.frequency
}

// GetLastModified returns the last time this collector definition was updated.
func (c *collector) GetLastModified() time.Time {
	return c.lastModified
}

// String implements fmt.Stringer
func (c *collector) String() string {
	return c.name
}
func (c *collector) Unregister() {
	for _, metric := range c.metrics {
		prometheus.Unregister(metric.vec)
	}
}

// WithFrequency determines how often the collector runs.
func (c *collector) WithFrequency(fr int) Collector {
	c.frequency = fr
	return c
}

// WithMaxResults sets the max number of results to return.
func (c *collector) WithMaxResults(max int) Collector {
	c.maxResults = max
	return c
}

func (c *collector) WithRegisterer(reg prometheus.Registerer) Collector {
	c.registerer = reg
	log.Infof("%+v", c.registerer)
	return c
}

// counterAdd adds the new value to the counter.
func (c *collector) counterAdd(
	vec *prometheus.CounterVec, name string, labels []string, value float64,
) {
	key, err := c.getKey(name, labels)
	if err != nil {
		return
	}
	var delta float64
	v, ok := c.metricsCache.Get(key)
	if ok {
		cv, _ := v.(cacheValue)
		if cv.value > value {
			delta = value
		} else {
			delta = value - cv.value
		}
	} else {
		delta = value
	}
	vec.WithLabelValues(labels...).Add(delta)
	c.metricsCache.Add(key, cacheValue{labels, vec, value})
}

// gaugeSet sets a gauge value.
func (c *collector) gaugeSet(
	vec *prometheus.GaugeVec, name string, labels []string, value float64,
) {
	key, err := c.getKey(name, labels)
	if err != nil {
		return
	}
	vec.WithLabelValues(labels...).Set(value)
	c.metricsCache.Add(key, cacheValue{labels, vec, value})
}

func (c *collector) getKey(name string, labels []string) (string, error) {
	key := cacheKey{
		Family: name,
		Labels: labels,
	}
	jsonKey, err := json.Marshal(key)
	if err != nil {
		return "", err
	}
	return string(jsonKey), nil
}
func (c *collector) resetGauges() {
	for _, m := range c.metrics {
		switch metric := m.vec.(type) {
		case *prometheus.GaugeVec:
			metric.Reset()
		}
	}
}
func (c *collector) maybeInitCache() {
	if c.metricsCache == nil {
		c.metricsCache = lru.New(c.cardinality * c.maxResults * 2)
		c.metricsCache.OnEvicted = func(key lru.Key, value interface{}) {
			labels := value.(cacheValue).labels
			vec := value.(cacheValue).vec
			vec.DeleteLabelValues(labels...)
		}
	}
}
