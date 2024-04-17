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
	"strings"
	"sync"
	"time"

	"github.com/cockroachlabs/visus/internal/server"
	"github.com/cockroachlabs/visus/internal/store"
	"github.com/nxadm/tail"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"
)

// Scanner scans logs and reports metrics based on defined patterns.
type Scanner interface {
	server.Task
	GetLastModified() time.Time
}

type metric struct {
	counter *prometheus.CounterVec
	regex   *regexp.Regexp
}

type scanner struct {
	config *Config
	target *store.Scan

	registerer prometheus.Registerer
	metrics    map[string]*metric
	mu         struct {
		sync.Mutex
		tail *tail.Tail
	}
}

// Config defines the behavior of the scanner
type Config struct {
	FromBeginning bool
	Follow        bool
	Reopen        bool
	Poll          bool
}

var crdbPrefix = regexp.MustCompile("^[FEIW][0-9][0-9][0-9][0-9][0-9][0-9] ")

// New creates a new scanner to collect the metrics.
func New(target *store.Scan, config *Config, register prometheus.Registerer) (Scanner, error) {
	scanner := &scanner{
		target:     target,
		registerer: register,
		metrics:    make(map[string]*metric),
	}
	for _, p := range target.Patterns {
		err := scanner.addCounter(p)
		if err != nil {
			return nil, err
		}
	}
	scanner.config = config
	return scanner, nil
}

// GetLastModified returns the last time the scan definition was modified.
func (s *scanner) GetLastModified() time.Time {
	return s.target.LastModified.Time
}

// Start starts to scan the logs.
func (s *scanner) Start(ctx context.Context) error {
	if s.getTail() != nil {
		return nil
	}
	if err := s.newTail(); err != nil {
		return err
	}
	switch s.target.Format {
	case store.CRDBV2:
		go s.scanCockroachLog()
	default:
		return errors.Errorf("format %q is not supported", s.target.Format)
	}
	return nil
}

// Stop stops the scanning activity.
func (s *scanner) Stop(_ context.Context) error {
	t := s.resetTail()
	if t == nil {
		return nil
	}
	defer t.Cleanup()
	return t.Stop()
}

// Stopped returns true if the scanner is done.
func (s *scanner) Stopped() bool {
	return s.getTail() == nil
}

// addCounter adds a metric counter. The name must match one of the columns returned by the query.
func (s *scanner) addCounter(pattern store.Pattern) error {
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
	log.Debugf("registering counter %s (%s)", metricName, pattern.Regex)
	s.metrics[pattern.Name] = &metric{
		counter: vec,
		regex:   regexp.MustCompile(pattern.Regex),
	}
	return nil
}

// scanCockroachLog scan a cockroach log.
func (s *scanner) scanCockroachLog() error {
	for line := range s.getTail().Lines {
		for _, m := range s.metrics {
			if !crdbPrefix.Match([]byte(line.Text)) {
				continue
			}
			if m.regex.Match([]byte(line.Text)) {
				fields := strings.Split(line.Text, " ")
				module := ""
				if len(fields) >= 4 {
					_, after, found := strings.Cut(fields[3], "@")
					if found {
						module, _, _ = strings.Cut(after, ":")
					} else {
						module, _, _ = strings.Cut(fields[3], ":")
					}
				}
				m.counter.WithLabelValues(string(line.Text[0]), module).Inc()
			}
		}
	}
	log.Infof("Done scanner for %s", s.target.Path)
	s.resetTail()
	return nil
}

func (s *scanner) getTail() *tail.Tail {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mu.tail
}

func (s *scanner) resetTail() *tail.Tail {
	s.mu.Lock()
	t := s.mu.tail
	defer s.mu.Unlock()
	s.mu.tail = nil
	return t
}

func (s *scanner) newTail() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var location *tail.SeekInfo
	if !s.config.FromBeginning {
		location = &tail.SeekInfo{
			Offset: 0,
			Whence: io.SeekEnd,
		}
	}
	var err error
	log.Tracef("Starting scanner for %s with config %+v", s.target.Path, *s.config)
	s.mu.tail, err = tail.TailFile(
		s.target.Path, tail.Config{
			Follow:   s.config.Follow,
			ReOpen:   s.config.Follow,
			Location: location,
			Poll:     s.config.Poll,
		})
	return err
}
