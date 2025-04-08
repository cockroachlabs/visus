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
	"io"
	"regexp"
	"sync"
	"time"

	"github.com/cockroachdb/field-eng-powertools/stopper"
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

// Match checks if the line satisfies the regex.
func (m *Metric) Match(line string) bool {
	return m.regex.Match([]byte(line))
}

// Scanner scans a log file to extract the given metrics.
type Scanner struct {
	config     *Config
	metrics    map[string]*Metric
	registerer prometheus.Registerer
	target     *store.Scan
	parser     Parser
	mu         struct {
		sync.RWMutex
		tail *tail.Tail
	}
}

// Parser implements the custom logic to parse a specific log file type.
type Parser interface {
	// Parse a single line and produce a metric if the line satisfy the metric filter.
	Parse(string, *Metric) error
}

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

	switch scan.Format {
	case store.CRDBv2Auth:
		scanner.parser = &CRDBv2AuthParser{}
		for _, p := range scan.Patterns {
			err := scanner.addCounter(p, []string{"user", "identity", "method", "transport", "event"})
			if err != nil {
				return nil, err
			}
		}
	case store.CRDBv2:
		scanner.parser = &CRDBv2Parser{}
		for _, p := range scan.Patterns {
			err := scanner.addCounter(p, []string{"level", "source"})
			if err != nil {
				return nil, err
			}
		}
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
	ctx.Go(func(ctx *stopper.Context) error {
		// Start to scan the file. By default wait for new lines to be
		// written to file, and retries if the file becomes inaccessible
		tail, err := s.scan()
		if err != nil {
			log.Errorf("Scanner %s failed. %s", s.target.Name, err.Error())
			return errors.WithStack(err)
		}
		log.Infof("Scanner %s started", s.target.Name)
		s.parse(tail)
		return nil
	})
	ctx.Go(func(ctx *stopper.Context) error {
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

// addCounter adds a metric counter to the Prometheus registry.
// The counter track the number of lines matching the pattern.
func (s *Scanner) addCounter(pattern store.Pattern, labels []string) error {
	metricName := s.target.Name + "_" + pattern.Name
	vec := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: metricName,
			Help: pattern.Help,
		},
		labels,
	)
	s.registerer.Unregister(vec)
	if err := s.registerer.Register(vec); err != nil {
		return err
	}
	regex, err := regexp.Compile(pattern.Regex)
	if err != nil {
		return err
	}
	log.Infof("registering counter %s (%s)", metricName, pattern.Regex)
	s.metrics[pattern.Name] = &Metric{
		counter: vec,
		regex:   regex,
	}
	return nil
}

// parse each line in the file.
// Errors are only logged without stopping processing the file.
func (s *Scanner) parse(tail *tail.Tail) {
	for line := range tail.Lines {
		for _, metric := range s.metrics {
			if err := s.parser.Parse(line.Text, metric); err != nil {
				errorCounts.WithLabelValues(tail.Filename).Inc()
				log.WithError(err).Error("failed to parse auth object", line.Num)
			}
		}
	}
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

// stopped returns true if the scanner is done.
func (s *Scanner) stopped() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.mu.tail == nil
}
