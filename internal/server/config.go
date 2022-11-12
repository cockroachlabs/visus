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

import (
	"crypto/tls"
	"crypto/x509"
	"net/url"
	"os"
	"time"
)

// Config encapsulates the command-line configurations and the logic
// necessary to make those values usable.
type Config struct {
	BindAddr          string        // Address to bind the server to.
	BindCert, BindKey string        // Paths to Certificate and Key.
	CaCert            string        // Path to the Root CA.
	Endpoint          string        // Endpoint for metrics
	Insecure          bool          // Sanity check to ensure that the operator really means it.
	Prometheus        string        // URL for the node prometheus endpoint
	Refresh           time.Duration // how often to refresh the configuration.
	URL               string        // URL to connect to the database
	ProcMetrics       bool          // Enable collections of process metrics.
	VisusMetrics      bool          // Enable collection of visus metrics.
}

// GetTLSClientConfig returns the TLS configuration to use for outgoing http connections.
func (c *Config) GetTLSClientConfig() (*tls.Config, error) {
	url, err := url.Parse(c.Prometheus)
	if err != nil {
		return nil, err
	}
	if url.Scheme == "http" {
		return nil, nil
	}
	var caCertPool *x509.CertPool
	if c.CaCert != "" {
		caCert, err := os.ReadFile(c.CaCert)
		if err != nil {
			return nil, err
		}
		caCertPool = x509.NewCertPool()
		caCertPool.AppendCertsFromPEM(caCert)
	}
	return &tls.Config{
		RootCAs: caCertPool,
	}, nil
}
