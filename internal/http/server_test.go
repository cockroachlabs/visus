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

// Package http implements an http server to export metrics in Prometheus format.
package http

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	_ "net/http/pprof"
	"os"
	"testing"
	"time"

	"github.com/cockroachdb/field-eng-powertools/stopper"
	"github.com/cockroachlabs/visus/internal/metric"
	"github.com/cockroachlabs/visus/internal/server"
	"github.com/cockroachlabs/visus/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRefreshHistograms(t *testing.T) {
	r := require.New(t)
	a := assert.New(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	stop := stopper.WithContext(ctx)
	mockStore := &store.Memory{}
	mockStore.Init(ctx)
	h1 := &store.Histogram{
		Enabled: true,
		Name:    "h1",
	}
	mockStore.PutHistogram(ctx, h1)
	writer, err := metric.NewWriter("file:///test", nil, nil)
	r.NoError(err)
	server := &serverImpl{
		clientTLSConfig: &clientTLSConfig{},
		keyPair:         &keyPair{},
		config: &server.Config{
			RewriteHistograms: true,
		},
		store:         mockStore,
		metricsWriter: writer,
	}
	err = server.refresh(stop)
	r.NoError(err)
	translators := server.metricsWriter.GetTranslators()
	a.Equal(1, len(translators))
	a.Equal(h1, translators[0].Histogram())

	// Verifying we load new enabled histograms.
	h2 := &store.Histogram{
		Enabled: true,
		Name:    "h2",
	}
	mockStore.PutHistogram(ctx, h2)
	err = server.refresh(stop)
	r.NoError(err)
	translators = server.metricsWriter.GetTranslators()
	a.Equal(2, len(translators))
	a.Equal(h1, translators[0].Histogram())
	a.Equal(h2, translators[1].Histogram())

	// Verifying we don't load histograms that are not enabled.
	h3 := &store.Histogram{
		Enabled: false,
		Name:    "h3",
	}
	mockStore.PutHistogram(ctx, h3)
	err = server.refresh(stop)
	r.NoError(err)
	translators = server.metricsWriter.GetTranslators()
	a.Equal(2, len(translators))
	a.Equal(h1, translators[0].Histogram())
	a.Equal(h2, translators[1].Histogram())

	// Verifying we remove deleted histograms.
	mockStore.DeleteHistogram(ctx, "h1")
	err = server.refresh(stop)
	r.NoError(err)
	translators = server.metricsWriter.GetTranslators()
	a.Equal(1, len(translators))
	a.Equal(h2, translators[0].Histogram())

	// If there is a error, we keep the old translators.
	mockStore.InjectError(errors.New("injected error"))
	err = server.refresh(stop)
	a.Error(err)
	translators = server.metricsWriter.GetTranslators()
	a.Equal(1, len(translators))
	a.Equal(h2, translators[0].Histogram())
}

func TestRefreshTlsConfig(t *testing.T) {
	r := require.New(t)
	a := assert.New(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	stop := stopper.WithContext(ctx)
	mockStore := &store.Memory{}
	mockStore.Init(ctx)
	writer, err := metric.NewWriter("file:///test", nil, nil)
	r.NoError(err)
	server := &serverImpl{
		clientTLSConfig: &clientTLSConfig{},
		keyPair:         &keyPair{},
		config: &server.Config{
			RewriteHistograms: true,
		},
		store:         mockStore,
		metricsWriter: writer,
	}
	err = server.refresh(stop)
	r.NoError(err)
	a.Equal(&clientTLSConfig{}, server.clientTLSConfig)
	a.Equal(&keyPair{}, server.keyPair)
	caPath := "testdata/ca.crt"
	server.clientTLSConfig.caPath = caPath
	certPath := "testdata/node.crt"
	keyPath := "testdata/node.key"
	server.keyPair = &keyPair{
		certPath: certPath,
		keyPath:  keyPath,
	}
	err = server.refresh(stop)
	r.NoError(err)

	expectedCa, err := os.ReadFile(caPath)
	r.NoError(err)
	expectedCertPool := x509.NewCertPool()
	expectedCertPool.AppendCertsFromPEM(expectedCa)
	expectedCert, err := tls.LoadX509KeyPair(certPath, keyPath)
	r.NoError(err)
	// Verify that we correctly load the CA
	a.True(expectedCertPool.Equal(server.clientTLSConfig.get().RootCAs))

	// Verify that we correctly load the Keypair
	cert, err := server.keyPair.getCertificateFunc()(nil)
	r.NoError(err)
	a.Equal(expectedCert.Certificate, cert.Certificate)

	caPath = "testdata/nothere_ca.crt"
	server.clientTLSConfig.caPath = caPath
	err = server.refresh(stop)
	a.Error(err)

	// Verify that the old CA is still there
	a.True(expectedCertPool.Equal(server.clientTLSConfig.get().RootCAs))

	certPath = "testdata/nothere.crt"
	keyPath = "testdata/nothere.key"
	server.keyPair.certPath = certPath
	server.keyPair.keyPath = keyPath
	err = server.refresh(stop)
	a.Error(err)

	// Verify that we are still using the old ones in case of an error.
	cert, err = server.keyPair.getCertificateFunc()(nil)
	r.NoError(err)
	a.Equal(expectedCert.Certificate, cert.Certificate)
}
