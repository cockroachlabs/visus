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

// Package translator provides the utilites to convert a HDR histogram to a log linear histogram.
package translator

import (
	"context"
	"io"
	"math"
	"regexp"

	"github.com/cockroachlabs/visus/internal/store"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	log "github.com/sirupsen/logrus"
)

// Translator translate histograms to a log linear format.
type Translator interface {
	// Translate histograms to a log linear format, and write the result to a writer.
	Translate(ctx context.Context, family *dto.MetricFamily, out io.Writer) error
	// Histogram returns a copy of the backing histogram definition.
	// Used for testing.
	Histogram() *store.Histogram
}

// Translator defines the property of a histogram filter.
type translator struct {
	histogram *store.Histogram
	include   *regexp.Regexp
}

// New instantiate a new translator from a histogram definition.
func New(h *store.Histogram) (Translator, error) {
	include, err := regexp.Compile(h.Regex)
	if err != nil {
		return nil, err
	}
	return &translator{
		histogram: h,
		include:   include,
	}, nil
}

// Load is a utility function that loads all the histogram configurations
// from the store and builds a Translator for each histogram
func Load(ctx context.Context, store store.Store) ([]Translator, error) {
	names, err := store.GetHistogramNames(ctx)
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return nil, nil
	}
	translators := make([]Translator, 0)
	for _, name := range names {
		histogram, err := store.GetHistogram(ctx, name)
		if err != nil {
			return nil, err
		}
		if histogram.Enabled {
			translator, err := New(histogram)
			if err != nil {
				return nil, err
			}
			translators = append(translators, translator)
		}
	}
	return translators, nil
}

// Histogram implements Translator
func (t *translator) Histogram() *store.Histogram {
	c := *t.histogram
	return &c
}

// Translate implements Translator
func (t *translator) Translate(ctx context.Context, family *dto.MetricFamily, out io.Writer) error {
	if family.GetType() == dto.MetricType_HISTOGRAM {
		if t.include.MatchString(family.GetName()) {
			log.Debugf("Translating %v", family.GetName())
			t.translate(family)
			_, err := expfmt.MetricFamilyToText(out, family)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// TranslateHistogram translates the HDR Histogram into a Log10 linear histogram
func (t *translator) translate(mf *dto.MetricFamily) {
	histogram := t.histogram
	log.Debugf("Translating %+v", histogram)
	bins := histogram.Bins
	for _, m := range mf.Metric {
		var prev *dto.Bucket = nil
		requiredBuckets := 1
		max := 0.0
		if len(m.Histogram.Bucket) >= 2 {
			if histogram.End > 0 {
				max = float64(histogram.End)
			} else {
				for _, b := range m.Histogram.Bucket {
					u := b.GetUpperBound()
					if u != math.Inf(1) && u > max {
						max = u
					}
				}
			}
			requiredBuckets = requiredBuckets + int(math.Ceil(math.Log10(float64(max))))*bins
		}
		newBuckets := make([]*dto.Bucket, 0, requiredBuckets)
		currLog10Bucket := createLog10Bucket(float64(histogram.Start), max, bins)
		for _, curr := range m.GetHistogram().GetBucket() {
			newBuckets = currLog10Bucket.addLog10Buckets(curr, prev, newBuckets)
			prev = curr
		}
		m.Histogram.Bucket = newBuckets
	}
}
