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

package histogram

import (
	_ "embed"
	"testing"

	"github.com/cockroachlabs/visus/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//go:embed testdata/malformed.yaml
var malformed string

// TestMarshalRoundTrip verifies we can marshal/unmarshall a collection configuration.
func TestMarshalRoundTrip(t *testing.T) {
	tests := []struct {
		name      string
		histogram *store.Histogram
		yaml      string
		wantErr   string
	}{
		{
			name: "good",
			histogram: &store.Histogram{
				Bins:  10,
				Name:  "histogram_01",
				Regex: "^sql_exec_latency$",
				Start: 1000000,
				End:   20000000000,
			},
		},
		{
			name: "no regex",
			histogram: &store.Histogram{
				Bins:  10,
				Name:  "histogram_01",
				Start: 1000000,
				End:   20000000000,
			},
			wantErr: "regex must be specified",
		},
		{
			name: "no name",
			histogram: &store.Histogram{
				Bins:  10,
				Regex: "^sql_exec_latency$",
				Start: 1000000,
				End:   20000000000,
			},
			wantErr: "name must be specified",
		},
		{
			name:    "unmarshal error",
			yaml:    malformed,
			wantErr: "cannot unmarshal",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := require.New(t)
			a := assert.New(t)
			var data []byte
			var err error
			switch {
			case test.histogram != nil:
				data, err = marshal(test.histogram)
				r.NoError(err)
			case test.yaml != "":
				data = []byte(test.yaml)
			default:
				panic("invalid test case")
			}
			out, err := unmarshal(data)
			if test.wantErr != "" {
				a.ErrorContains(err, test.wantErr)
				return
			}
			r.NoError(err)
			a.Equal(test.histogram, out)
		})
	}
}
