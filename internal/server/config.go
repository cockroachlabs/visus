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

package server

import "time"

// Config encapsulates the command-line parameters that can be supplied
// to configure the behavior of Visus.
type Config struct {
	AllowUnsafeInternals bool          // Enable allow_unsafe_internals for database connections.
	BindAddr             string        // Address to bind the server to.
	BindCert, BindKey    string        // Paths to Certificate and Key.
	CaCert               string        // Path to the Root CA.
	Endpoint             string        // Endpoint for metrics
	Inotify              bool          // Enable inotify for scans.
	Insecure             bool          // Sanity check to ensure that the operator really means it.
	ProcMetrics          bool          // Enable collections of process metrics.
	Prometheus           string        // URL for the node prometheus endpoint
	Refresh              time.Duration // how often to refresh the configuration.
	RewriteHistograms    bool          // Enable histogram rewriting
	URL                  string        // URL to connect to the database
	VisusMetrics         bool          // Enable collection of Visus metrics.
}
