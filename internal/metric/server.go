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

// Package metric retrieves metric values on a schedule.
package metric

import (
	"context"
	"time"

	"github.com/cockroachlabs/visus/internal/collector"
	"github.com/cockroachlabs/visus/internal/database"
	"github.com/cockroachlabs/visus/internal/server"
	"github.com/cockroachlabs/visus/internal/store"
	"github.com/go-co-op/gocron"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	log "github.com/sirupsen/logrus"
)

type scheduledJob struct {
	collector collector.Collector
	job       *gocron.Job
}

// Config defines the metrics to be retrieved from the metricsServer
type metricsServer struct {
	config           *server.Config
	conn             database.Connection
	store            store.Store
	scheduledJobs    map[string]*scheduledJob
	scheduler        *gocron.Scheduler
	registry         *prometheus.Registry
	collectorCount   *prometheus.CounterVec
	collectorErrors  *prometheus.CounterVec
	collectorLatency *prometheus.HistogramVec
}

// New creates a new server to collect the metrics.
func New(
	ctx context.Context,
	cfg *server.Config,
	store store.Store,
	conn database.Connection,
	registry *prometheus.Registry,
) server.Server {
	var collectorCounts, collectorErrors *prometheus.CounterVec
	var collectorLatency *prometheus.HistogramVec
	if cfg.VisusMetrics {
		labels := []string{"collector"}
		collectorCounts = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "visus_collector_count",
				Help: "number of times a collector has been executed",
			},
			labels,
		)
		err := registry.Register(collectorCounts)
		if err != nil {
			log.Panicf("Unable to register metric %e", err)
		}
		collectorErrors = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "visus_collector_errors",
				Help: "number of errors in collector executions",
			},
			labels,
		)
		err = registry.Register(collectorErrors)
		if err != nil {
			log.Panicf("Unable to register metric %e", err)
		}
		collectorLatency = prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "visus_collector_latency",
				Help:    "amount of time in milliseconds elapsed to run a collector",
				Buckets: prometheus.ExponentialBucketsRange(1, 1000, 8),
			},
			labels,
		)
		err = registry.Register(collectorLatency)
		if err != nil {
			log.Panicf("Unable to register metric %e", err)
		}
	}
	if cfg.ProcMetrics {
		registry.Register(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	}
	server := &metricsServer{
		config:           cfg,
		conn:             conn,
		store:            store,
		collectorCount:   collectorCounts,
		collectorErrors:  collectorErrors,
		collectorLatency: collectorLatency,
		scheduler:        gocron.NewScheduler(time.UTC),
		scheduledJobs:    make(map[string]*scheduledJob),
		registry:         registry,
	}
	return server
}

// addJob add a new collector to the scheduler.
func (m *metricsServer) addJob(name string, coll collector.Collector, job *gocron.Job) {
	m.scheduledJobs[name] = &scheduledJob{
		collector: coll,
		job:       job,
	}
}

// refresh schedules all the collectors based on the
// configuration stored in the database.
func (m *metricsServer) Refresh(ctx context.Context) error {
	newCollectors := make(map[string]bool)
	names, err := m.store.GetCollectionNames(ctx)
	if err != nil {
		return err
	}
	for _, name := range names {
		coll, err := m.store.GetCollection(ctx, name)
		if err != nil {
			log.Errorf("Unable to find %s: %s", name, err.Error())
			continue
		}
		if !coll.Enabled {
			continue
		}
		newCollectors[name] = true
		existing, found := m.scheduledJobs[coll.Name]
		if found && !existing.collector.GetLastModified().Before(coll.LastModified.Time) {
			log.Debugf("Already scheduled %s, no change", coll.Name)
			continue
		}
		if found {
			log.Debugf("Already scheduled %s, removing", coll.Name)
			m.scheduler.RemoveByReference(existing.job)
		}
		collctr, err := collector.FromCollection(coll, m.conn, m.registry)
		if err != nil {
			log.Errorf("Error scheduling collector %s: %s", name, err.Error())
			continue
		}
		log.Infof("Scheduling collector %s every %d seconds", collctr.String(), collctr.GetFrequency())
		job, err := m.scheduler.Every(collctr.GetFrequency()).Second().
			Do(func(collctr collector.Collector) {
				name := collctr.String()
				log.Debugf("Running collector %s", name)
				start := time.Now()
				err := collctr.Collect(ctx, m.conn)
				if err != nil {
					if m.collectorErrors != nil {
						m.collectorErrors.WithLabelValues(name).Inc()
					}
					log.Errorf("collector %s: %s", collctr, err.Error())
				}
				if m.collectorCount != nil {
					elapsed := time.Since(start).Milliseconds()
					m.collectorLatency.WithLabelValues(name).Observe(float64(elapsed))
					m.collectorCount.WithLabelValues(name).Inc()
				}
			}, collctr)
		if err != nil {
			return err
		}
		m.addJob(coll.Name, collctr, job)
	}
	// remove all the schedule jobs that have been deleted from the database.
	for key, value := range m.scheduledJobs {
		if _, ok := newCollectors[key]; !ok {
			log.Infof("Removing collector: %s", key)
			m.scheduler.RemoveByReference(value.job)
			value.collector.Unregister()
			delete(m.scheduledJobs, key)
		}
	}
	return nil
}

// Start schedules all the collectors.
func (m *metricsServer) Start(ctx context.Context) error {
	m.scheduler.Every(m.config.Refresh).
		Do(func() {
			m.Refresh(ctx)
		})
	m.scheduler.StartAsync()
	return nil
}

// Shutdown gracefully stops all the collectors.
func (m *metricsServer) Shutdown(ctx context.Context) error {
	m.scheduler.Stop()
	m.scheduler.Clear()
	return nil
}
