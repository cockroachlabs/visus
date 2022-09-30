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

package config

import (
	"crypto/tls"
	"path"

	"github.com/pkg/errors"
)

// Config encapsulates the command-line configurations and the logic
// necessary to make those values usable.
type Config struct {
	BindAddr string // Address to bind the proxy to.
	CertsDir string // Paths to TLS configurations.
	Endpoint string // Endpoint for metrics
	Insecure bool   // Sanity check to ensure that the operator really means it.
	URL      string // URL to connect to the database
}

// TLSConfig returns the TLS configuration to use for incoming SQL
// connections. It will return nil if TLS should not be used for
// incoming connections.
func (c *Config) TLSConfig() (*tls.Config, error) {
	if c.CertsDir != "" {
		cert, err := tls.LoadX509KeyPair(path.Join(c.CertsDir, "client.insights.crt"), path.Join(c.CertsDir, "client.insights.key"))
		if err != nil {
			return nil, err
		}
		return &tls.Config{Certificates: []tls.Certificate{cert}}, nil
	}
	if c.Insecure {
		return nil, nil
	}
	return nil, errors.New("no --certs-dir, must specify --insecure")
}
