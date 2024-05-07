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
	"crypto/x509"
	"encoding/pem"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestClientTLSConfig verifies that we can read CA certificates from the filesystem.
func TestClientTLSConfig(t *testing.T) {
	a := assert.New(t)
	r := require.New(t)
	config := &clientTLSConfig{
		caPath: "testdata/ca.crt",
	}

	err := config.load()
	r.NoError(err)
	roots := config.get()
	buff, err := os.ReadFile("testdata/node.crt")
	r.NoError(err)
	block, _ := pem.Decode(buff)
	cert, err := x509.ParseCertificate(block.Bytes)
	r.NoError(err)

	opts := x509.VerifyOptions{
		DNSName: "localhost",
		Roots:   roots.RootCAs,
	}
	_, err = cert.Verify(opts)
	a.NoError(err)

	opts = x509.VerifyOptions{
		DNSName: "other",
		Roots:   roots.RootCAs,
	}
	_, err = cert.Verify(opts)
	a.ErrorContains(err, "certificate is valid for localhost")
}

// TestKeyPair verifies that we can read a key pair from the filesystem.
func TestKeyPair(t *testing.T) {
	a := assert.New(t)
	r := require.New(t)
	kp := &keyPair{
		certPath: "testdata/node.crt",
		keyPath:  "testdata/node.key",
	}

	err := kp.load()
	r.NoError(err)
	f := kp.getCertificateFunc()
	tls, err := f(nil)
	r.NoError(err)
	cert, err := x509.ParseCertificate(tls.Certificate[0])
	r.NoError(err)
	a.Equal("node", cert.Subject.CommonName)

	kp = &keyPair{
		certPath: "testdata/ca.crt",
		keyPath:  "testdata/node.key",
	}

	err = kp.load()
	a.ErrorContains(err, "private key does not match public key")

}
