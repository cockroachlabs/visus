// Copyright 2024 Cockroach Labs Inc.
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

package scanner

import (
	"sync"

	"github.com/cockroachdb/field-eng-powertools/stopper"
	"github.com/cockroachlabs/visus/internal/server"
	"github.com/cockroachlabs/visus/internal/store"
	"github.com/go-co-op/gocron"
	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"
)

type scannerServer struct {
	config        *server.Config       // configuration
	fromBeginning bool                 // used for testing, read logs from beginning.
	registry      *prometheus.Registry // metrics registry
	scheduler     *gocron.Scheduler    // the scheduler for refreshing the configuration.
	store         store.Store          // store that contains the configuration of the scans.

	mu struct {
		sync.RWMutex
		// TODO (silvano): refactor scanners and metrics.scheduledJobs
		// there is a lot of repeated code.
		scanners map[string]*Scanner
	}
}

var _ server.Server = &scannerServer{}

// New returns a server that manages scanners.
func New(
	cfg *server.Config, store store.Store, registry *prometheus.Registry, scheduler *gocron.Scheduler,
) server.Server {
	scanners := &scannerServer{
		config:    cfg,
		registry:  registry,
		scheduler: scheduler,
		store:     store,
	}
	scanners.mu.scanners = make(map[string]*Scanner)
	return scanners
}

// Refresh implements server.Server
func (s *scannerServer) Refresh(ctx *stopper.Context) error {
	log.Info("Refreshing scanners configuration")
	err := s.refresh(ctx)
	if err != nil {
		server.RefreshErrors.WithLabelValues("scanners").Inc()
	}
	server.RefreshCounts.WithLabelValues("scanners").Inc()
	return nil
}

// Start implements server.Server.
func (s *scannerServer) Start(ctx *stopper.Context) error {
	// If we don't have a scheduler, we force a refresh and we are done.
	if s.scheduler == nil {
		return s.Refresh(ctx)
	}
	_, err := s.scheduler.Every(s.config.Refresh).
		Do(func() {
			err := s.Refresh(ctx)
			if err != nil {
				log.Errorf("Error refreshing scanners %s", err.Error())
			}
		})
	return err
}

// add the named scanner
func (s *scannerServer) add(name string, scanner *Scanner) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mu.scanners[name] = scanner
}

// buildScanner creates a new scanner.
func (s *scannerServer) buildScanner(name string, scan *store.Scan) (*Scanner, error) {
	scanner, err := FromConfig(scan, &Config{
		Follow:        true,
		FromBeginning: s.fromBeginning,
		Poll:          !s.config.Inotify,
		Reopen:        true,
	}, s.registry)
	if err != nil {
		return nil, err
	}
	s.add(name, scanner)
	return scanner, nil
}

// cleanup removes all the scanners that we don't need to keep.
func (s *scannerServer) cleanup(toKeep map[string]bool) (err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, value := range s.mu.scanners {
		if _, ok := toKeep[key]; !ok {
			log.Infof("Removing scanner %s", key)
			err = value.Stop()
			if err != nil {
				return
			}
			delete(s.mu.scanners, key)
		}
	}
	return
}

// delete the named scanner
func (s *scannerServer) delete(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.mu.scanners, name)
}

// get returns the named scanner.
func (s *scannerServer) get(name string) (*Scanner, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sc, ok := s.mu.scanners[name]
	return sc, ok
}

// refresh the scanners configuration.
func (s *scannerServer) refresh(ctx *stopper.Context) error {
	newScans := make(map[string]bool)
	names, err := s.store.GetScanNames(ctx)
	if err != nil {
		return err
	}
	log.Info("Refreshing scanners")
	for _, name := range names {
		scan, err := s.store.GetScan(ctx, name)
		if err != nil {
			log.Errorf("Unable to find %s: %s", name, err.Error())
			continue
		}
		log.Debugf("Considering %s; enabled=%t;", name, scan.Enabled)
		if !scan.Enabled {
			continue
		}
		newScans[name] = true
		existing, found := s.get(name)
		if found && !existing.GetLastModified().Before(scan.LastModified.Time) {
			log.Debugf("Already scheduled %s, no change", scan.Name)
			continue
		}
		if found {
			log.Infof("Configuration for %s has changed", scan.Name)
			existing.Stop()
		}
		scanner, err := s.buildScanner(scan.Name, scan)
		if err != nil {
			log.Errorf("Error adding scanner %s: %s", name, err.Error())
			continue
		}
		if err := scanner.Start(ctx); err != nil {
			s.delete(scan.Name)
			log.Errorf("Error starting scanner %s: %s", name, err.Error())
		}
	}
	return s.cleanup(newScans)
}
