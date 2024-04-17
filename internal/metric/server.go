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
	"sync"
	"time"

	"github.com/cockroachlabs/visus/internal/collector"
	"github.com/cockroachlabs/visus/internal/database"
	"github.com/cockroachlabs/visus/internal/scanner"
	"github.com/cockroachlabs/visus/internal/server"
	"github.com/cockroachlabs/visus/internal/store"
	"github.com/go-co-op/gocron"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	log "github.com/sirupsen/logrus"
)

var (
	labels        = []string{"collector"}
	refreshCounts = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "visus_refresh_count",
			Help: "number of times refresh has been executed",
		},
	)
	collectorCounts = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "visus_collector_count",
			Help: "number of times a collector has been executed",
		},
		labels,
	)
	collectorErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "visus_collector_errors",
			Help: "number of errors in collector executions",
		},
		labels,
	)
	collectorLatency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "visus_collector_latency",
			Help:    "amount of time in milliseconds elapsed to run a collector",
			Buckets: prometheus.ExponentialBucketsRange(1, 1000, 8),
		},
		labels,
	)
	collectorMetrics = []prometheus.Collector{
		refreshCounts, collectorCounts, collectorErrors, collectorLatency,
	}
)

type scheduledJob struct {
	collector collector.Collector
	job       *gocron.Job
}

// Config defines the metrics to be retrieved from the metricsServer
type metricsServer struct {
	config *server.Config
	conn   database.Connection
	store  store.Store

	registry  *prometheus.Registry
	scheduler *gocron.Scheduler
	mu        struct {
		sync.RWMutex
		scanners      map[string]scanner.Scanner
		scheduledJobs map[string]*scheduledJob
		stopped       bool
	}
}

// New creates a new server to collect the metrics.
func New(
	ctx context.Context,
	cfg *server.Config,
	store store.Store,
	conn database.Connection,
	registry *prometheus.Registry,
) server.Server {

	if cfg.VisusMetrics {
		for _, coll := range collectorMetrics {
			err := registry.Register(coll)
			if err != nil {
				log.Panicf("Unable to register metric %e", err)
			}
		}
	}
	if cfg.ProcMetrics {
		registry.Register(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	}
	server := &metricsServer{
		config:    cfg,
		conn:      conn,
		store:     store,
		scheduler: gocron.NewScheduler(time.UTC),
		registry:  registry,
	}
	server.mu.stopped = true
	server.mu.scanners = make(map[string]scanner.Scanner)
	server.mu.scheduledJobs = make(map[string]*scheduledJob)

	return server
}

func (m *metricsServer) Refresh(ctx context.Context) error {
	if err := m.refreshCollectors(ctx); err != nil {
		return err
	}
	return m.refreshScanners(ctx)
}

// Start schedules all the collectors.
func (m *metricsServer) Start(ctx context.Context) error {
	if !m.setStopped(true) {
		log.Info("already running")
		return nil
	}
	m.scheduler.StartAsync()
	job, err := m.scheduler.Every(m.config.Refresh).
		Do(func() {
			select {
			case <-ctx.Done():
				log.Debug("Refresh job done")
				return
			default:
				log.Info("Refreshing metrics")
				err := m.Refresh(ctx)
				if err != nil {
					log.Errorf("Error refreshing metrics %s", err.Error())
				}
				refreshCounts.Inc()
				log.Debugf("Done refreshing metrics")
			}
		})
	log.Debugf("Starting refresh job %s", job.GetName())
	return err
}

// Stop gracefully stops all the collectors.
func (m *metricsServer) Stop(_ context.Context) error {
	if m.setStopped(true) {
		return nil
	}
	m.scheduler.Stop()
	m.scheduler.Clear()
	return nil
}

func (m *metricsServer) Stopped() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.mu.stopped
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
	for key, value := range m.mu.scheduledJobs {
		if _, ok := toKeep[key]; !ok {
			log.Infof("Removing collector: %s", key)
			m.scheduler.RemoveByReference(value.job)
			value.collector.Unregister()
			m.deleteJob(key)
		}
	}
}

// deleteJob deletes the named job
func (m *metricsServer) deleteJob(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.mu.scheduledJobs, name)
}

// getJob the named job.
func (m *metricsServer) getJob(name string) (*scheduledJob, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	job, ok := m.mu.scheduledJobs[name]
	return job, ok
}

// refreshCollectors schedules all the collectors based on the
// configuration stored in the database.
func (m *metricsServer) refreshCollectors(ctx context.Context) error {
	newCollectors := make(map[string]bool)
	names, err := m.store.GetCollectionNames(ctx)
	if err != nil {
		return err
	}
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
					if m.config.VisusMetrics {
						collectorErrors.WithLabelValues(name).Inc()
					}
					log.Errorf("collector %s: %s", collctr, err.Error())
				}
				if m.config.VisusMetrics {
					elapsed := time.Since(start).Milliseconds()
					collectorLatency.WithLabelValues(name).Observe(float64(elapsed))
					collectorCounts.WithLabelValues(name).Inc()
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

// addScanner add a new scan to the scheduler.
func (m *metricsServer) addScanner(name string, scan *store.Scan) (scanner.Scanner, error) {
	scanner, err := scanner.New(scan, &scanner.Config{
		FromBeginning: false,
		Poll:          !m.config.Inotify,
		Follow:        true,
		Reopen:        true,
	}, m.registry)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mu.scanners[name] = scanner
	return scanner, nil
}

// cleanupScanners removes all the scanners that we don't need to keep.
func (m *metricsServer) cleanupScanners(ctx context.Context, toKeep map[string]bool) (err error) {
	for key, value := range m.mu.scanners {
		if _, ok := toKeep[key]; !ok {
			log.Infof("Removing scanner %s", key)
			err = value.Stop(ctx)
			if err != nil {
				return
			}
			m.deleteScanner(key)
		}
	}
	return
}

// deleteScanner deletes the named scanner
func (m *metricsServer) deleteScanner(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.mu.scanners, name)
}

// getScanner returns the named scanner.
func (m *metricsServer) getScanner(name string) (scanner.Scanner, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	sc, ok := m.mu.scanners[name]
	return sc, ok
}

// refreshScanners schedules all the scanners based on the
// configuration stored in the database.
func (m *metricsServer) refreshScanners(ctx context.Context) error {
	newScans := make(map[string]bool)
	names, err := m.store.GetScanNames(ctx)
	if err != nil {
		return err
	}
	for _, name := range names {
		scan, err := m.store.GetScan(ctx, name)
		log.Debugf("Considering %s; enabled=%t;", name, scan.Enabled)
		if err != nil {
			log.Errorf("Unable to find %s: %s", name, err.Error())
			continue
		}
		if !scan.Enabled {
			continue
		}
		newScans[name] = true
		existing, found := m.getScanner(name)
		if found && !existing.GetLastModified().Before(scan.LastModified.Time) && !existing.Stopped() {
			log.Debugf("Already scheduled %s, no change", scan.Name)
			continue
		}
		if found {
			log.Infof("Configuration for %s has changed", scan.Name)
			existing.Stop(ctx)
		}
		scanner, err := m.addScanner(scan.Name, scan)
		if err != nil {
			log.Errorf("Error adding scanner %s: %s", name, err.Error())
			continue
		}
		if err := scanner.Start(ctx); err != nil {
			m.deleteScanner(scan.Name)
			log.Errorf("Error starting scanner %s: %s", name, err.Error())
		}
		log.Infof("Started scanner %s", scan.Name)
	}
	return m.cleanupScanners(ctx, newScans)
}

// setStopped sets the stopped status returning its current value.
func (m *metricsServer) setStopped(new bool) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	current := m.mu.stopped
	m.mu.stopped = new
	return current
}
