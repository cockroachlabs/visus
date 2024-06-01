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

package translator

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/cockroachlabs/visus/internal/store"
	"github.com/prometheus/common/expfmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//go:embed testdata/input.txt
var input string

//go:embed testdata/output.txt
var output string

//go:embed testdata/multistore.txt
var multistore string

//go:embed testdata/multistoreout.txt
var multistoreout string

func TestHistogramConversion(t *testing.T) {
	assert := assert.New(t)
	var parser expfmt.TextParser
	histogram := &store.Histogram{
		Start: 100,
		Bins:  10}
	translator := &translator{
		histogram: histogram,
	}
	metricFamilies, _ := parser.TextToMetricFamilies(strings.NewReader(input))

	for _, mf := range metricFamilies {
		translator.translate(mf)
		var buf bytes.Buffer
		expfmt.MetricFamilyToText(&buf, mf)
		assert.Equal(output, buf.String())
	}
}

func newHistogram(name string, enabled bool) *store.Histogram {
	return &store.Histogram{
		Bins:    10,
		Enabled: enabled,
		End:     1e9,
		Name:    name,
		Regex:   fmt.Sprintf("^%s$", name),
		Start:   1e6,
	}
}

// TestLoad verifies we can load histograms from store and building
// translators for enabled histograms.
func TestLoad(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tests := []struct {
		name       string
		histograms []*store.Histogram
	}{
		{
			"none",
			nil,
		},
		{
			"one",
			[]*store.Histogram{newHistogram("one", true)},
		},
		{
			"two",
			[]*store.Histogram{newHistogram("one", true), newHistogram("two", true)},
		},
		{
			"one enabled",
			[]*store.Histogram{newHistogram("one", true), newHistogram("two", false)},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			a := assert.New(t)
			st := &store.Memory{}
			st.Init(ctx)
			want := make([]*store.Histogram, 0)
			for _, h := range tt.histograms {
				if h.Enabled {
					want = append(want, h)
				}
				err := st.PutHistogram(ctx, h)
				r.NoError(err)
			}
			translators, err := Load(ctx, st)
			r.NoError(err)
			r.Equal(len(want), len(translators))
			for idx, h := range want {
				tr, ok := translators[idx].(*translator)
				r.True(ok)
				a.Equal(h, tr.histogram)
				regex := regexp.MustCompile(h.Regex)
				a.Equal(regex, tr.include)
			}
		})
	}
}

func TestMultiStoreConversion(t *testing.T) {
	assert := assert.New(t)
	var parser expfmt.TextParser
	histogram := &store.Histogram{
		Start: 100,
		Bins:  10}
	translator := &translator{
		histogram: histogram,
	}
	metricFamilies, _ := parser.TextToMetricFamilies(strings.NewReader(multistore))
	for _, mf := range metricFamilies {
		translator.translate(mf)
		var buf bytes.Buffer
		expfmt.MetricFamilyToText(&buf, mf)
		assert.Equal(multistoreout, buf.String())
	}
}

func TestIdentityConversion(t *testing.T) {
	assert := assert.New(t)
	var parser expfmt.TextParser
	histogram := &store.Histogram{
		Start: 100,
		Bins:  10}
	translator := &translator{
		histogram: histogram,
	}
	metricFamilies, _ := parser.TextToMetricFamilies(strings.NewReader(output))
	for _, mf := range metricFamilies {
		translator.translate(mf)
		var buf bytes.Buffer
		expfmt.MetricFamilyToText(&buf, mf)
		assert.Equal(output, buf.String())
	}

}
