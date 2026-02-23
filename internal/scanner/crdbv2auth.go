// Copyright 2025 Cockroach Labs Inc.
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
	"encoding/json"
	"regexp"
	"strings"
)

type authFields struct {
	EventType      string
	InstanceID     int64
	Method         string
	SystemIdentity string
	Timestamp      int64
	Transport      string
	User           string
}

var jsonRegex = regexp.MustCompile(`={"\w+":`)

// CRDBv2AuthParser is a parser for cockroach-sql-auth logs.
type CRDBv2AuthParser struct{}

var _ Parser = &CRDBv2AuthParser{}

// Parse implements Parser.
func (c *CRDBv2AuthParser) Parse(line []byte, metric *Metric) error {
	if !crdbPrefix.Match(line) {
		return nil
	}
	if metric.Match(line) {
		labels, err := parseAuthLine(line)
		if err != nil {
			return err
		}
		if labels != nil {
			metric.counter.WithLabelValues(
				labels.User,
				labels.SystemIdentity,
				labels.Method,
				labels.Transport,
				labels.EventType).Inc()
		}
	}
	return nil
}

// trimUser cleans up user names.
func trimUser(s string) string {
	s = strings.TrimPrefix(s, "‹")
	s = strings.TrimSuffix(s, "›")
	return s
}

// parseAuthLine parse a line from the auth log.
func parseAuthLine(line []byte) (*authFields, error) {
	// Sample log entry (on one line):
	// I250407 20:23:15.882051 732501 4@util/log/event_log.go:39 ⋮
	// [T1,Vsystem,n1,client=127.0.0.1:34104,hostssl,user=‹craig›]
	// 18 ={"Timestamp":1744057395882047431,
	// "EventType":"client_authentication_ok",
	// "InstanceID":1,"Network":"tcp",
	// "RemoteAddress":"‹127.0.0.1:34104›",
	// "SessionID":"183422f60d3dc0180000000000000001",
	// "Transport":"hostssl",
	// "User":"‹craig›","SystemIdentity":"‹craig›",
	// "Method":"cert-password"}

	// We are extracting the json payload.
	jsonStart := jsonRegex.FindIndex(line)
	if jsonStart == nil {
		return nil, nil
	}
	payload := line[jsonStart[0]+1:]
	fields := &authFields{}
	if err := json.Unmarshal(payload, fields); err != nil {
		return nil, err
	}
	fields.User = trimUser(fields.User)
	fields.SystemIdentity = trimUser(fields.SystemIdentity)
	return fields, nil
}
