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
	"net/http"
	"net/http/httptest"
	_ "net/http/pprof"
	"os"
	"testing"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/cockroachdb/field-eng-powertools/stopper"
	"github.com/cockroachlabs/visus/internal/metric"
	"github.com/cockroachlabs/visus/internal/server"
	"github.com/cockroachlabs/visus/internal/store"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// disconnectedResponseWriter simulates a client that has disconnected: every
// write fails, mimicking the "client disconnected" error returned by the HTTP/2
// server once the peer is gone. It records whether WriteHeader was called so
// tests can assert the handler does not try to send an error status back to a
// client that is no longer listening.
type disconnectedResponseWriter struct {
	header      http.Header
	wroteHeader bool
	statusCode  int
}

var _ http.ResponseWriter = &disconnectedResponseWriter{}

func (d *disconnectedResponseWriter) Header() http.Header {
	if d.header == nil {
		d.header = http.Header{}
	}
	return d.header
}

func (d *disconnectedResponseWriter) Write([]byte) (int, error) {
	return 0, errors.New("client disconnected")
}

func (d *disconnectedResponseWriter) WriteHeader(statusCode int) {
	d.wroteHeader = true
	d.statusCode = statusCode
}

// TestRefreshHistograms verifies we reload the histograms from the store.
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

// TestRefreshNoHistograms verifies that refresh works with no histograms
func TestRefreshNoHistograms(t *testing.T) {
	a := assert.New(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	stop := stopper.WithContext(ctx)
	mockStore := &store.Memory{}
	mockStore.Init(ctx)
	server := &serverImpl{
		clientTLSConfig: &clientTLSConfig{},
		keyPair:         &keyPair{},
		config: &server.Config{
			RewriteHistograms: false,
		},
		store: mockStore,
	}
	err := server.refresh(stop)
	a.NoError(err)
}

// TestRefreshTLSConfig verifies we can reload the TLS configuration
func TestRefreshTLSConfig(t *testing.T) {
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

// newTestRegistry returns a registry with a single registered gauge so the
// metrics handler has something to gather.
func newTestRegistry(t *testing.T) *prometheus.Registry {
	t.Helper()
	registry := prometheus.NewRegistry()
	gauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "visus_test_gauge",
		Help: "gauge used by the metrics handler tests",
	})
	gauge.Set(1)
	require.NoError(t, registry.Register(gauge))
	return registry
}

// TestMetricsHandler verifies that a normal scrape returns the gathered metrics.
func TestMetricsHandler(t *testing.T) {
	a := assert.New(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	stop := stopper.WithContext(ctx)
	server := &serverImpl{
		registry: newTestRegistry(t),
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	server.metricsHandler(stop)(rec, req)
	a.Equal(http.StatusOK, rec.Code)
	a.Contains(rec.Body.String(), "visus_test_gauge")
}

// TestMetricsHandlerClientDisconnect verifies that when the client disconnects
// mid-response the handler stops writing and does not try to send an error
// status back to the client that is no longer listening.
func TestMetricsHandlerClientDisconnect(t *testing.T) {
	a := assert.New(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	stop := stopper.WithContext(ctx)
	server := &serverImpl{
		registry: newTestRegistry(t),
	}
	w := &disconnectedResponseWriter{}
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	// The handler must not panic and must not attempt to write an error status
	// back to the disconnected client.
	server.metricsHandler(stop)(w, req)
	a.False(w.wroteHeader, "handler should not write a status back to a disconnected client")
}

// TestMetricsHandlerCopyError verifies that when copying from the upstream
// source fails the handler returns early without writing gathered metrics.
func TestMetricsHandlerCopyError(t *testing.T) {
	r := require.New(t)
	a := assert.New(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	stop := stopper.WithContext(ctx)
	// A file source that does not exist makes Copy fail on open.
	writer, err := metric.NewWriter("file:///nonexistent-source.txt", nil, nil)
	r.NoError(err)
	server := &serverImpl{
		registry:      newTestRegistry(t),
		metricsWriter: writer,
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	server.metricsHandler(stop)(rec, req)
	// Copy failed, so we return before gathering registry metrics.
	a.NotContains(rec.Body.String(), "visus_test_gauge")
}
