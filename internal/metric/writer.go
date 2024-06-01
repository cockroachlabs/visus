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

// Package metric provides utilities to rewrite metrics based on a set of
// translators
package metric

import (
	"context"
	"crypto/tls"
	"io"
	"net/http"
	"net/url"
	"os"
	"sync"

	"github.com/cockroachlabs/visus/internal/translator"
	"github.com/pkg/errors"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
)

// Writer copies metrics from a source to destination, applying a set of translators
type Writer struct {
	open func(context.Context) (io.ReadCloser, error)
	mu   struct {
		sync.RWMutex
		translators []translator.Translator
	}
}

// HTTPClient submits requests to an HTTP server.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// NewWriter builds a new writer that fetches metrics from the named location,
// using the named translators.
func NewWriter(
	URL string, tlsConfig *tls.Config, translators []translator.Translator,
) (*Writer, error) {
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
	}
	return newWriter(URL, client, translators)
}

// newWriter builds a new writer that fetches metrics from the named location,
// using the named translators and http client. It allows to inject a mock http
// client for testing.
func newWriter(
	URL string, client HTTPClient, translators []translator.Translator,
) (*Writer, error) {
	writer := &Writer{}
	writer.mu.translators = translators
	parsed, err := url.Parse(URL)
	if err != nil {
		return nil, err
	}
	switch parsed.Scheme {
	case "file":
		writer.open = func(context.Context) (io.ReadCloser, error) {
			return os.Open(parsed.Path)
		}
	case "http", "https":
		writer.open = func(ctx context.Context) (io.ReadCloser, error) {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, URL, nil)
			if err != nil {
				return nil, err
			}

			response, err := client.Do(req)
			if err != nil {
				return nil, err
			}
			if response.StatusCode != http.StatusOK {
				return nil, errors.New(response.Status)
			}
			return response.Body, nil
		}
	default:
		return nil, errors.Errorf("%s protocol not supported", parsed.Scheme)
	}
	return writer, nil
}

// Copy reads the metrics from source, applies the translators and outputs
// the resulting metrics to the named writer.
func (w *Writer) Copy(ctx context.Context, out io.Writer) error {
	in, err := w.open(ctx)
	if err != nil {
		return err
	}
	defer in.Close()
	var parser expfmt.TextParser
	metricFamilies, err := parser.TextToMetricFamilies(in)
	if err != nil {
		return err
	}
	translators := w.GetTranslators()
	for _, mf := range metricFamilies {
		if mf.GetType() == dto.MetricType_HISTOGRAM && translators != nil {
			for _, translator := range translators {
				translator.Translate(ctx, mf, out)
			}
		} else {
			_, err := expfmt.MetricFamilyToText(out, mf)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// GetTranslators returns the translators.
func (w *Writer) GetTranslators() []translator.Translator {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.mu.translators
}

// SetTranslators replaces the translators with new ones.
func (w *Writer) SetTranslators(translators []translator.Translator) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.mu.translators = translators
}
