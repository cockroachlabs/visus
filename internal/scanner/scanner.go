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

// Package scanner parses log files
package scanner

import (
	"context"
	"io"
	"regexp"
	"sync"
	"time"

	"github.com/cockroachlabs/visus/internal/stopper"
	"github.com/cockroachlabs/visus/internal/store"
	"github.com/nxadm/tail"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"
)

// Config defines the behavior of the scanner
type Config struct {
	Follow        bool
	FromBeginning bool
	Poll          bool
	Reopen        bool
}

// Metric defines the prometheus counter to increment when a line matches the regular expression.
type Metric struct {
	counter *prometheus.CounterVec
	regex   *regexp.Regexp
}

// Scanner scans a log file to extract the given metrics.
type Scanner struct {
	config     *Config
	metrics    map[string]*Metric
	registerer prometheus.Registerer
	target     *store.Scan
	parser     Parse
	mu         struct {
		sync.RWMutex
		tail *tail.Tail
	}
}

// Parse implements the custom logic to parse a specific log file type.
type Parse func(context.Context, *tail.Tail, map[string]*Metric) error

// FromConfig creates a new scanner, based on the configuration provided.
func FromConfig(
	scan *store.Scan, config *Config, registerer prometheus.Registerer,
) (*Scanner, error) {
	scanner := &Scanner{
		config:     config,
		metrics:    make(map[string]*Metric),
		registerer: registerer,
		target:     scan,
	}
	for _, p := range scan.Patterns {
		err := scanner.addCounter(p)
		if err != nil {
			return nil, err
		}
	}
	switch scan.Format {
	case store.CRDBV2:
		scanner.parser = scanCockroachLog
	default:
		return nil, errors.Errorf("format not supported %s", scan.Format)
	}
	return scanner, nil
}

// GetLastModified returns the last time the scanner configuration was updated.
func (s *Scanner) GetLastModified() time.Time {
	return s.target.LastModified.Time
}

// Start scanning the log
func (s *Scanner) Start(ctx *stopper.Context) error {
	ctx.Go(func() error {
		// Start to scan the file. By default wait for new lines to be
		// written to file, and retries if the file becomes inaccessible
		tail, err := s.scan()
		if err != nil {
			log.Errorf("Scanner %s failed. %s", s.target.Name, err.Error())
			return errors.WithStack(err)
		}
		log.Infof("Scanner %s started", s.target.Name)
		err = s.parser(ctx, tail, s.metrics)
		if err != nil {
			log.Errorf("Scanner %s failed. %s", s.target.Name, err.Error())
			errors.WithStack(err)
		}
		return nil
	})
	ctx.Go(func() error {
		<-ctx.Stopping()
		s.Stop()
		log.Infof("Scanner %s stopped", s.target.Name)
		return nil
	})
	return nil
}

// Stop stops the scanning activity.
func (s *Scanner) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.mu.tail == nil {
		return nil
	}
	t := s.mu.tail
	s.mu.tail = nil
	defer t.Cleanup()
	return t.Stop()
}

// Stopped returns true if the scanner is done.
func (s *Scanner) Stopped() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.mu.tail == nil
}

// addCounter adds a metric counter to the Prometheus registry.
// The counter track the number of lines matching the pattern.
func (s *Scanner) addCounter(pattern store.Pattern) error {
	metricName := s.target.Name + "_" + pattern.Name
	vec := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: metricName,
			Help: pattern.Help,
		},
		[]string{"level", "source"},
	)
	s.registerer.Unregister(vec)
	if err := s.registerer.Register(vec); err != nil {
		return err
	}
	regex, err := regexp.Compile(pattern.Regex)
	if err != nil {
		return err
	}
	log.Debugf("registering counter %s (%s)", metricName, pattern.Regex)
	s.metrics[pattern.Name] = &Metric{
		counter: vec,
		regex:   regex,
	}
	return nil
}

// scan opens the file and parses new lines as they are written to the file.
func (s *Scanner) scan() (*tail.Tail, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.mu.tail != nil {
		return nil, errors.Errorf("scanner already running for %s", s.target.Path)
	}
	var location *tail.SeekInfo
	if !s.config.FromBeginning {
		location = &tail.SeekInfo{
			Offset: 0,
			Whence: io.SeekEnd,
		}
	}
	var err error
	s.mu.tail, err = tail.TailFile(
		s.target.Path, tail.Config{
			Logger:   log.StandardLogger(),
			Follow:   s.config.Follow,
			ReOpen:   s.config.Follow,
			Location: location,
			Poll:     s.config.Poll,
		})
	return s.mu.tail, err
}
