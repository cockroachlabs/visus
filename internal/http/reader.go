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

package http

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
)

func fetch(ctx context.Context, URL string, tlsConfig *tls.Config) (*http.Response, error) {
	transport := &http.Transport{}
	transport.TLSClientConfig = tlsConfig
	client := http.Client{
		Transport: transport,
	}

	req, err := http.NewRequest(http.MethodGet, URL, nil)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	return client.Do(req)
}

// ReadMetrics reads the metrics from the endpoint and returns a map of dto.MetricFamily
func ReadMetrics(
	ctx context.Context, URL string, tlsConfig *tls.Config,
) (map[string]*dto.MetricFamily, error) {
	data, err := fetch(ctx, URL, tlsConfig)
	if err != nil {
		return nil, err
	}
	defer data.Body.Close()
	if data.StatusCode != http.StatusOK {
		return nil, errors.New(data.Status)
	}
	var parser expfmt.TextParser
	return parser.TextToMetricFamilies(data.Body)
}
