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

package metric

import (
	"context"
	"time"

	"github.com/cockroachlabs/visus/internal/collector"
	"github.com/cockroachlabs/visus/internal/config"
	"github.com/cockroachlabs/visus/internal/database"
	"github.com/cockroachlabs/visus/internal/server"
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
func New(ctx context.Context, cfg *config.Config, pool database.PgxPool) (server.Server, error) {
	server := &metricsServer{
		pool:          pool,
		scheduler:     gocron.NewScheduler(time.UTC),
		scheduledJobs: make(map[string]*scheduledJob),
	}
	server.AddDefaultCollectors()
	return server, nil
}

func (s *metricsServer) AddCollector(coll collector.Collector) {
	s.scheduledJobs[coll.String()] = &scheduledJob{
		collector: coll,
	}
}

func (s *metricsServer) AddDefaultCollectors() {
	for _, coll := range collector.GetDefaultCollectors(s.pool) {
		s.AddCollector(coll)
	}
}

func (s *metricsServer) Start(ctx context.Context) error {
	for _, scheduledJob := range s.scheduledJobs {
		job, err := s.scheduler.Every(scheduledJob.collector.GetFrequency()).Second().
			Do(func(coll collector.Collector) {
				err := coll.Collect(ctx)
				if err != nil {
					log.Errorf("collector %s: %s", coll, err.Error())
				}
			}, scheduledJob.collector)
		if err != nil {
			return err
		}
		scheduledJob.job = job
	}
	s.scheduler.StartAsync()
	return nil
}
func (s *metricsServer) Shutdown(ctx context.Context) error {
	s.scheduler.Stop()
	s.scheduler.Clear()
	return nil
}
