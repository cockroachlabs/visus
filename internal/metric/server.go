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
	log "github.com/sirupsen/logrus"
)

type scheduledJob struct {
	collector collector.Collector
	job       *gocron.Job
}

// Config defines the metrics to be retrieved from the metricsServer
type metricsServer struct {
	pool          database.PgxPool
	scheduledJobs map[string]*scheduledJob
	scheduler     *gocron.Scheduler
}

// New creates a new server to collect the metrics.
func New(ctx context.Context, cfg *server.Config, pool database.PgxPool) server.Server {
	server := &metricsServer{
		pool:          pool,
		scheduler:     gocron.NewScheduler(time.UTC),
		scheduledJobs: make(map[string]*scheduledJob),
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

// scheduleCollectors schedules all the collectors based on the
// configuration stored in the database.
func (m *metricsServer) scheduleCollectors(ctx context.Context) error {
	newCollectors := make(map[string]bool)
	names, err := store.GetCollectionNames(ctx, m.pool)
	if err != nil {
		return err
	}
	for _, name := range names {
		coll, err := store.GetCollection(ctx, m.pool, name)
		if err != nil {
			log.Errorf("Unable to find %s: %s", name, err.Error())
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
		collctr := collector.FromCollection(coll, m.pool)
		log.Infof("Scheduling %s every %d seconds", collctr.String(), collctr.GetFrequency())
		job, err := m.scheduler.Every(collctr.GetFrequency()).Second().
			Do(func(collctr collector.Collector) {
				log.Infof("Running collector %s", collctr.String())
				err := collctr.Collect(ctx)
				if err != nil {
					log.Errorf("collector %s: %s", collctr, err.Error())
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
			log.Infof("Removing %s", key)
			m.scheduler.RemoveByReference(value.job)
			value.collector.Unregister()
			delete(m.scheduledJobs, key)
		}
	}
	return nil
}

// Start schedules all the collectors.
func (m *metricsServer) Start(ctx context.Context) error {
	m.scheduler.Every(1).Minute().
		Do(func() {
			m.scheduleCollectors(ctx)
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
