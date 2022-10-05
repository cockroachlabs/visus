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
	"io"

	"github.com/cockroachlabs/visus/internal/translator"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
)

// WriteMetrics writes the metrics, converting HDR Histogram into Log10 linear histograms.
func WriteMetrics(
	ctx context.Context,
	metricFamilies map[string]*dto.MetricFamily,
	translators []translator.Translator,
	out io.Writer,
) error {
	for _, mf := range metricFamilies {
		if mf.GetType() == dto.MetricType_HISTOGRAM {
			for _, histogram := range translators {
				histogram.Translate(ctx, mf, out)
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
