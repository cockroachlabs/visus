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
	"sync"
	"time"

	"github.com/cockroachdb/field-eng-powertools/stopper"
	"github.com/cockroachlabs/visus/internal/database"
	"github.com/cockroachlabs/visus/internal/server"
	"github.com/cockroachlabs/visus/internal/store"
	"github.com/go-co-op/gocron"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	log "github.com/sirupsen/logrus"
)

var (
	labels          = []string{"collector"}
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
		collectorCounts, collectorErrors, collectorLatency,
	}
)

type scheduledJob struct {
	collector Collector
	job       *gocron.Job
}

// serverImpl manages the collections.
type serverImpl struct {
	config    *server.Config
	conn      database.Connection
	registry  *prometheus.Registry
	scheduler *gocron.Scheduler
	store     store.Store

	mu struct {
		sync.RWMutex
		scheduledJobs map[string]*scheduledJob
		stopped       bool
	}
}

// New creates a new server to collect the metrics defined in the collections.
func New(
	cfg *server.Config,
	store store.Store,
	conn database.Connection,
	registry *prometheus.Registry,
	scheduler *gocron.Scheduler,
) (server.Server, error) {
	if cfg.VisusMetrics {
		for _, coll := range collectorMetrics {
			if err := registry.Register(coll); err != nil {
				return nil, err
			}
		}
		if err := registry.Register(collectors.NewGoCollector()); err != nil {
			return nil, err
		}
	}
	if cfg.ProcMetrics {
		registry.Register(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	}
	server := &serverImpl{
		config:    cfg,
		conn:      conn,
		registry:  registry,
		scheduler: scheduler,
		store:     store,
	}
	server.mu.stopped = true
	server.mu.scheduledJobs = make(map[string]*scheduledJob)
	return server, nil
}

// Refresh implements server.Server
func (s *serverImpl) Refresh(ctx *stopper.Context) error {
	log.Info("Refreshing metrics collector configuration")
	err := s.refresh(ctx)
	if err != nil {
		server.RefreshErrors.WithLabelValues("metric_collector").Inc()
		return err
	}
	server.RefreshCounts.WithLabelValues("metric_collector").Inc()
	return nil
}

// Start implements server.Server.
func (s *serverImpl) Start(ctx *stopper.Context) error {
	// If we don't have a scheduler, we force a refresh and we are done.
	if s.scheduler == nil {
		return s.Refresh(ctx)
	}
	_, err := s.scheduler.Every(s.config.Refresh).
		Do(func() {
			err := s.Refresh(ctx)
			if err != nil {
				log.Errorf("Error refreshing metrics %s", err.Error())
			}
		})
	return err
}

// addJob add a new collector to the scheduler.
func (s *serverImpl) addJob(name string, coll Collector, job *gocron.Job) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mu.scheduledJobs[name] = &scheduledJob{
		collector: coll,
		job:       job,
	}
}

// cleanupJobs removes all the jobs that we don't need to keep.
func (s *serverImpl) cleanupJobs(toKeep map[string]bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, value := range s.mu.scheduledJobs {
		if _, ok := toKeep[key]; !ok {
			log.Infof("Removing collector: %s", key)
			s.scheduler.RemoveByReference(value.job)
			value.collector.Unregister()
			delete(s.mu.scheduledJobs, key)
		}
	}
}

// getJob the named job.
func (s *serverImpl) getJob(name string) (*scheduledJob, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	job, ok := s.mu.scheduledJobs[name]
	return job, ok
}

func (s *serverImpl) refresh(ctx *stopper.Context) error {
	newCollectors := make(map[string]bool)
	names, err := s.store.GetCollectionNames(ctx)
	if err != nil {
		return err
	}
	log.Info("Refreshing metrics")
	last := time.Now().UTC().Add(-s.config.Refresh)
	isMainNode, err := s.store.IsMainNode(ctx, last)
	if err != nil {
		return err
	}
	for _, name := range names {
		coll, err := s.store.GetCollection(ctx, name)
		if err != nil {
			log.Errorf("Unable to find %s: %s", name, err.Error())
			continue
		}
		if !coll.Enabled || (coll.Scope == store.Cluster && !isMainNode) {
			log.Infof("Skipping %s; enabled: %t; scope: %s; main node: %t", name, coll.Enabled, coll.Scope, isMainNode)
			continue
		}
		newCollectors[name] = true
		existing, found := s.getJob(coll.Name)
		if found && !existing.collector.GetLastModified().Before(coll.LastModified.Time) {
			log.Debugf("Already scheduled %s", coll.Name)
			continue
		}
		if found {
			log.Infof("Configuration for %s has changed", coll.Name)
			s.scheduler.RemoveByReference(existing.job)
		}
		collctr, err := FromCollection(coll, s.registry)
		if err != nil {
			log.Errorf("Error scheduling collector %s: %s", name, err.Error())
			continue
		}
		log.Infof("Scheduling collector %s every %d seconds", collctr.String(), collctr.GetFrequency())
		job, err := s.scheduler.Every(collctr.GetFrequency()).Second().
			Do(func(collctr Collector) {
				name := collctr.String()
				log.Tracef("Running collector %s", name)
				start := time.Now()
				err := collctr.Collect(ctx, s.conn)
				if err != nil {
					if s.config.VisusMetrics {
						collectorErrors.WithLabelValues(name).Inc()
					}
					log.Errorf("collector %s: %s", collctr, err.Error())
				}
				if s.config.VisusMetrics {
					elapsed := time.Since(start).Milliseconds()
					collectorLatency.WithLabelValues(name).Observe(float64(elapsed))
					collectorCounts.WithLabelValues(name).Inc()
				}
			}, collctr)
		if err != nil {
			log.Errorf("error scheduling collector %s: %s", coll.Name, err.Error())
			continue
		}
		s.addJob(coll.Name, collctr, job)
	}
	s.cleanupJobs(newCollectors)
	return nil
}
