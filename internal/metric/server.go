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
	"sync"
	"time"

	"github.com/cockroachlabs/visus/internal/collector"
	"github.com/cockroachlabs/visus/internal/database"
	"github.com/cockroachlabs/visus/internal/server"
	"github.com/cockroachlabs/visus/internal/stopper"
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
	scheduler        *gocron.Scheduler
	registry         *prometheus.Registry
	collectorCount   *prometheus.CounterVec
	collectorErrors  *prometheus.CounterVec
	collectorLatency *prometheus.HistogramVec
	mu               struct {
		sync.RWMutex
		scheduledJobs map[string]*scheduledJob
		stopped       bool
	}
}

// New creates a new server to collect the metrics.
func New(
	cfg *server.Config,
	store store.Store,
	conn database.Connection,
	registry *prometheus.Registry,
	scheduler *gocron.Scheduler,
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
		collectorCount:   collectorCounts,
		collectorErrors:  collectorErrors,
		collectorLatency: collectorLatency,
		config:           cfg,
		conn:             conn,
		registry:         registry,
		scheduler:        scheduler,
		store:            store,
	}

	server.mu.stopped = true
	server.mu.scheduledJobs = make(map[string]*scheduledJob)
	return server
}

// Refresh implements server.Server
func (m *metricsServer) Refresh(ctx *stopper.Context) error {
	newCollectors := make(map[string]bool)
	names, err := m.store.GetCollectionNames(ctx)
	if err != nil {
		return err
	}
	log.Info("Refreshing metrics")
	last := time.Now().UTC().Add(-m.config.Refresh)
	isMainNode, err := m.store.IsMainNode(ctx, last)
	if err != nil {
		return err
	}
	for _, name := range names {
		coll, err := m.store.GetCollection(ctx, name)
		if err != nil {
			log.Errorf("Unable to find %s: %s", name, err.Error())
			continue
		}
		if !coll.Enabled || (coll.Scope == store.Cluster && !isMainNode) {
			log.Infof("Skipping %s; enabled: %t; scope: %s; main node: %t", name, coll.Enabled, coll.Scope, isMainNode)
			continue
		}
		newCollectors[name] = true
		existing, found := m.getJob(coll.Name)
		if found && !existing.collector.GetLastModified().Before(coll.LastModified.Time) {
			log.Debugf("Already scheduled %s", coll.Name)
			continue
		}
		if found {
			log.Infof("Configuration for %s has changed", coll.Name)
			m.scheduler.RemoveByReference(existing.job)
		}
		collctr, err := collector.FromCollection(coll, m.registry)
		if err != nil {
			log.Errorf("Error scheduling collector %s: %s", name, err.Error())
			continue
		}
		log.Infof("Scheduling collector %s every %d seconds", collctr.String(), collctr.GetFrequency())
		job, err := m.scheduler.Every(collctr.GetFrequency()).Second().
			Do(func(collctr collector.Collector) {
				name := collctr.String()
				start := time.Now()
				err := collctr.Collect(ctx, m.conn)
				if err != nil {
					if m.collectorErrors != nil {
						m.collectorErrors.WithLabelValues(name).Inc()
					}
					log.Errorf("collector %s: %s", collctr, err.Error())
				}
				if m.config.VisusMetrics {
					elapsed := time.Since(start).Milliseconds()
					m.collectorLatency.WithLabelValues(name).Observe(float64(elapsed))
					m.collectorCount.WithLabelValues(name).Inc()
				}
			}, collctr)
		if err != nil {
			log.Errorf("error scheduling collector %s: %s", coll.Name, err.Error())
			continue
		}
		m.addJob(coll.Name, collctr, job)
	}
	m.cleanupJobs(newCollectors)
	return nil
}

// Start schedules all the collectors.
func (m *metricsServer) Start(ctx *stopper.Context) error {
	// If we don't have a scheduler, we force a refresh and we are done.
	// Used for testing.
	if m.scheduler == nil {
		return m.Refresh(ctx)
	}
	_, err := m.scheduler.Every(m.config.Refresh).
		Do(func() {
			err := m.Refresh(ctx)
			if err != nil {
				log.Errorf("Error refreshing metrics %s", err.Error())
			}
		})
	return err
}

// addJob add a new collector to the scheduler.
func (m *metricsServer) addJob(name string, coll collector.Collector, job *gocron.Job) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mu.scheduledJobs[name] = &scheduledJob{
		collector: coll,
		job:       job,
	}
}

// cleanupJobs removes all the jobs that we don't need to keep.
func (m *metricsServer) cleanupJobs(toKeep map[string]bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for key, value := range m.mu.scheduledJobs {
		if _, ok := toKeep[key]; !ok {
			log.Infof("Removing collector: %s", key)
			m.scheduler.RemoveByReference(value.job)
			value.collector.Unregister()
			delete(m.mu.scheduledJobs, key)
		}
	}
}

// getJob the named job.
func (m *metricsServer) getJob(name string) (*scheduledJob, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	job, ok := m.mu.scheduledJobs[name]
	return job, ok
}
