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
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/cockroachlabs/visus/internal/database"
	"github.com/golang/groupcache/lru"
	"github.com/jackc/pgtype"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	log "github.com/sirupsen/logrus"
)

// A Collector retrieves all the metric values for all the gauges and counters configured.
type Collector interface {
	fmt.Stringer
	AddCounter(name string, desc string) Collector
	// AddGauge adds a gauge to the list of metrics to return.
	AddGauge(name string, desc string) Collector
	// GetFrequency returns how often the collector is called, in seconds.
	GetFrequency() int
	// Collect retrieves the values for all the metrics defined.
	Collect(ctx context.Context) error
	// WithFrequency determines how often the collector is called, in seconds.
	WithFrequency(frequency int) Collector
	// WithMaxResults sets the maximum number of results to collect each time Collect is called.
	WithMaxResults(max int) Collector
	// AddCounter adds a counter to the list of metrics to return.
}

type collector struct {
	name         string
	cardinality  int
	frequency    int
	labels       []string
	metrics      []metric
	query        string
	first        bool
	pool         database.PgxPool
	metricsCache *lru.Cache
	maxResults   int
	mu           struct {
		sync.Mutex
		inUse bool
	}
}

//go:generate go run golang.org/x/tools/cmd/stringer -type=MetricKind
// MetricKind represents the type of metric
type MetricKind int

const (
	// Undefined is for metrics not currently supported.
	Undefined MetricKind = iota
	// A counter is a cumulative metric that represents a single monotonically increasing value.
	Counter
	// A gauge is a metric that represents a single numerical value that can arbitrarily go up and down.
	Gauge
)

type metric struct {
	desc string
	kind MetricKind
	name string
	vec  interface{}
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

func New(name string, labels []string, query string, pool database.PgxPool) Collector {
	return &collector{
		name:       name,
		labels:     labels,
		query:      query,
		first:      true,
		frequency:  10,
		maxResults: 100,
		pool:       pool,
	}
}

func (c *collector) AddCounter(name string, desc string) Collector {
	c.cardinality++
	c.metrics = append(c.metrics,
		metric{
			desc: desc,
			kind: Counter,
			name: name,
			vec: promauto.NewCounterVec(
				prometheus.CounterOpts{
					Name: name,
					Help: desc,
				},
				c.labels,
			),
		})
	return c
}

func (c *collector) AddGauge(name string, desc string) Collector {
	c.cardinality++
	c.metrics = append(c.metrics,
		metric{
			desc: desc,
			kind: Gauge,
			name: name,
			vec: promauto.NewGaugeVec(
				prometheus.GaugeOpts{
					Name: name,
					Help: desc,
				},
				c.labels,
			),
		})
	return c
}

func (c *collector) GetFrequency() int {
	return c.frequency
}

func (c *collector) Collect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.mu.inUse {
		return nil
	}
	c.mu.inUse = true
	defer func() {
		c.mu.inUse = false
	}()
	c.maybeInitCache()
	query := c.query
	labelNames := c.labels
	log.Debugf("collectFamilyMetrics %s ", c.name)
	log.Tracef("collectFamilyMetrics %s query %s ", c.name, query)
	rows, err := c.pool.Query(ctx, query, c.maxResults)
	if err != nil {
		log.Errorf("collectFamilyMetrics %s ", err.Error())
		return err
	}
	defer rows.Close()

	desc := rows.FieldDescriptions()
	if len(desc) != len(c.labels)+len(c.metrics) {
		return errors.New("columns returned in the query must be match labels+metrics")
	}
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			log.Warnf("collectFamilyMetrics %s", err.Error())
			continue
		}
		log.Tracef("value %v ", values)
		labels := make([]string, 0)
		for i, v := range values {
			log.Tracef("value %T ", v)
			if i < len(labelNames) {
				if value, ok := v.(string); ok {
					log.Tracef("adding label %s ", value)
					labels = append(labels, value)
				} else {
					log.Errorf("Unknown type %T", v)
				}
			} else {
				if i-len(labelNames) >= len(c.metrics) {
					continue
				}
				metric := c.metrics[i-len(labelNames)]
				log.Tracef("Retrieving metric %T %T ", metric.vec, v)
				switch vec := metric.vec.(type) {
				case *prometheus.CounterVec:
					switch value := v.(type) {
					case float64:
						c.counterAdd(vec, metric.name, labels, value)
					case int:
						c.counterAdd(vec, metric.name, labels, float64(value))
					case pgtype.Numeric:
						var floatValue float64
						value.AssignTo(&floatValue)
						c.counterAdd(vec, metric.name, labels, floatValue)
					default:
						log.Errorf("Unknown type %T", v)
					}
				case *prometheus.GaugeVec:
					switch value := v.(type) {
					case float64:
						c.gaugeSet(vec, metric.name, labels, value)
					case int:
						c.gaugeSet(vec, metric.name, labels, float64(value))
					case pgtype.Numeric:
						var floatValue float64
						value.AssignTo(&floatValue)
						c.gaugeSet(vec, metric.name, labels, floatValue)
					default:
						log.Errorf("Unknown type %T", v)
					}
				}
			}
		}
	}
	return nil
}

func (c *collector) String() string {
	return c.name
}
func (c *collector) WithFrequency(fr int) Collector {
	c.frequency = fr
	return c
}
func (c *collector) WithMaxResults(max int) Collector {
	c.maxResults = max
	return c
}

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
