// Copyright 2026 Cockroach Labs Inc.
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

package scan

import (
	_ "embed"
	"testing"

	"github.com/cockroachlabs/visus/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//go:embed testdata/invalid_format.yaml
var invalidFormat string

//go:embed testdata/malformed.yaml
var malformed string

// TestMarshalRoundTrip verifies we can marshal/unmarshal a scan configuration.
func TestMarshalRoundTrip(t *testing.T) {
	tests := []struct {
		name    string
		scan    *store.Scan
		yaml    string
		wantErr string
	}{
		{
			name: "good crdb-v2",
			scan: &store.Scan{
				Enabled: true,
				Format:  store.CRDBv2,
				Name:    "scan_01",
				Path:    "testdata/sample.log",
				Patterns: []store.Pattern{
					{
						Help: "description",
						Name: "metric",
					},
				},
			},
		},
		{
			name: "good crdb-v2-auth",
			scan: &store.Scan{
				Enabled: true,
				Format:  store.CRDBv2Auth,
				Name:    "scan_02",
				Path:    "testdata/auth.log",
				Patterns: []store.Pattern{
					{
						Help: "auth attempts",
						Name: "auth",
					},
				},
			},
		},
		{
			name: "no name",
			scan: &store.Scan{
				Enabled: true,
				Format:  store.CRDBv2,
				Patterns: []store.Pattern{
					{
						Help: "description",
						Name: "metric",
					},
				},
			},
			wantErr: "name must be specified",
		},
		{
			name:    "invalid format",
			yaml:    invalidFormat,
			wantErr: "invalid format",
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
			case test.scan != nil:
				data, err = marshal(test.scan)
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
			a.Equal(test.scan, out)
		})
	}
}
