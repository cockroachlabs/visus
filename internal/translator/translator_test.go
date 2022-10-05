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
	_ "embed"
	"strings"
	"testing"

	"github.com/cockroachlabs/visus/internal/store"
	"github.com/prometheus/common/expfmt"
	"github.com/stretchr/testify/assert"
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
	histogram := store.Histogram{
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

func TestMultiStoreConversion(t *testing.T) {
	assert := assert.New(t)
	var parser expfmt.TextParser
	histogram := store.Histogram{
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
	histogram := store.Histogram{
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
