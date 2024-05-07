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

package http

import (
	"crypto/tls"
	"crypto/x509"
	"os"
	"sync"

	log "github.com/sirupsen/logrus"
)

type clientTLSConfig struct {
	caPath string
	mu     struct {
		sync.RWMutex
		tlsConfig *tls.Config
	}
}

func (c *clientTLSConfig) load() error {
	if c.caPath == "" {
		return nil
	}
	log.Debugf("reloading root CA from %s", c.caPath)
	caCert, err := os.ReadFile(c.caPath)
	if err != nil {
		return err
	}
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.mu.tlsConfig = &tls.Config{
		RootCAs: caCertPool,
	}
	return nil
}

func (c *clientTLSConfig) get() *tls.Config {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.mu.tlsConfig
}

type keyPair struct {
	certPath, keyPath string
	mu                struct {
		sync.RWMutex
		cert *tls.Certificate
	}
}

func (k *keyPair) load() error {
	if k.certPath == "" && k.keyPath == "" {
		return nil
	}
	log.Debugf("reloading key pair from %s %s", k.certPath, k.keyPath)
	cert, err := tls.LoadX509KeyPair(k.certPath, k.keyPath)
	if err != nil {
		return err
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	k.mu.cert = &cert
	return nil
}

func (k *keyPair) getCertificateFunc() func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	return func(_ *tls.ClientHelloInfo) (*tls.Certificate, error) {
		k.mu.RLock()
		defer k.mu.RUnlock()
		return k.mu.cert, nil
	}
}
