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
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cockroachlabs/visus/internal/store"
	"github.com/cockroachlabs/visus/internal/translator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//go:embed testdata/input.txt
var input string

//go:embed testdata/output.txt
var output string

type mockHTTPClient struct {
	file string
}

var _ HTTPClient = &mockHTTPClient{}

func (m *mockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	r, err := os.Open(m.file)
	if err != nil {
		return &http.Response{
			Status:     "500 Internal server error",
			StatusCode: http.StatusInternalServerError,
		}, nil
	}
	return &http.Response{
		Status:     "200 OK",
		StatusCode: http.StatusOK,
		Body:       r,
	}, nil
}

func fileURL(file string) string {
	ex, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	return fmt.Sprintf("file:///%s", filepath.Clean(filepath.Join(filepath.Dir(ex), "metric", file)))
}

func translators(histograms []*store.Histogram) []translator.Translator {
	if histograms == nil {
		return nil
	}
	translators := make([]translator.Translator, 0)
	for _, h := range histograms {
		t, err := translator.New(h)
		if err != nil {
			panic(err)
		}
		translators = append(translators, t)
	}
	return translators
}

func TestWriter(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tests := []struct {
		name        string
		URL         string
		client      HTTPClient
		translators []translator.Translator
		wantOut     string
		wantError   string
	}{
		{
			name:    "no op",
			URL:     fileURL("testdata/input.txt"),
			wantOut: input,
		},
		{
			name:      "file error",
			URL:       fileURL("testdata/not_there.txt"),
			wantError: "no such file or director",
		},
		{
			name: "file",
			URL:  fileURL("testdata/input.txt"),
			translators: translators([]*store.Histogram{
				{
					Start: 100,
					Bins:  10,
				},
			}),
			wantOut: output,
		},
		{
			name: "http",
			URL:  "http://test.txt",
			client: &mockHTTPClient{
				file: "testdata/input.txt",
			},
			translators: translators([]*store.Histogram{
				{
					Start: 100,
					Bins:  10,
				},
			}),
			wantOut: output,
		},
		{
			name: "http error",
			URL:  "http://test.txt",
			client: &mockHTTPClient{
				file: "testdata/not there.txt",
			},
			translators: translators([]*store.Histogram{
				{
					Start: 100,
					Bins:  10,
				},
			}),
			wantError: "Internal server error",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			a := assert.New(t)
			writer, err := newWriter(tt.URL, tt.client, tt.translators)
			r.NoError(err)
			buf := new(bytes.Buffer)
			err = writer.Copy(ctx, buf)
			if tt.wantError != "" {
				a.ErrorContains(err, tt.wantError)
				return
			}
			r.NoError(err)
			a.Equal(tt.wantOut, buf.String())
		})
	}
}
