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
	"context"
	"regexp"
	"strings"

	"github.com/nxadm/tail"
)

var crdbPrefix = regexp.MustCompile("^[FEIW][0-9][0-9][0-9][0-9][0-9][0-9] ")

// scanCockroachLog scan a cockroach log.
func scanCockroachLog(_ context.Context, tail *tail.Tail, metrics map[string]*Metric) error {
	for line := range tail.Lines {
		for _, m := range metrics {
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
	return nil
}
