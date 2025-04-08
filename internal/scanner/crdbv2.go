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
	"regexp"
	"strings"
)

var crdbPrefix = regexp.MustCompile("^[FEIW][0-9][0-9][0-9][0-9][0-9][0-9] ")

// CRDBv2Parser parses CockroachDB logs.
type CRDBv2Parser struct{}

var _ Parser = &CRDBv2Parser{}

// Parse implements Parser.
func (c *CRDBv2Parser) Parse(line string, metric *Metric) error {
	if !crdbPrefix.Match([]byte(line)) {
		return nil
	}
	if metric.Match(line) {
		fields := strings.Split(line, " ")
		module := ""
		if len(fields) >= 4 {
			_, after, found := strings.Cut(fields[3], "@")
			if found {
				module, _, _ = strings.Cut(after, ":")
			} else {
				module, _, _ = strings.Cut(fields[3], ":")
			}
		}
		metric.counter.WithLabelValues(string(line[0]), module).Inc()
	}
	return nil
}
