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

package collection

import (
	_ "embed"
	"testing"

	"github.com/cockroachlabs/visus/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//go:embed testdata/invalid_scope.yaml
var invalidScope string

//go:embed testdata/malformed.yaml
var malformed string

// TestMarshalRoundTrip verifies we can marshal/unmarshall a collection configuration.
func TestMarshalRoundTrip(t *testing.T) {
	tests := []struct {
		name       string
		collection *store.Collection
		yaml       string
		wantErr    string
	}{
		{
			name: "good",
			collection: &store.Collection{
				Frequency: pgtype.Interval{Microseconds: 1e6, Valid: true},
				Labels:    []string{"database"},
				MaxResult: 1,
				Metrics: []store.Metric{
					{
						Name: "queries",
						Kind: store.Counter,
						Help: "total queries per database",
					},
				},
				Name:  "collection_01",
				Query: "SELECT database,queries FROM stats LIMIT $1",
				Scope: store.Node,
			},
		},
		{
			name: "no query",
			collection: &store.Collection{
				Frequency: pgtype.Interval{Microseconds: 1e6, Valid: true},
				Labels:    []string{"database"},
				MaxResult: 1,
				Metrics: []store.Metric{
					{
						Name: "queries",
						Kind: store.Counter,
						Help: "total queries per database",
					},
				},
				Name:  "collection_01",
				Scope: store.Node,
			},
			wantErr: "query must be specified",
		},
		{
			name: "no name",
			collection: &store.Collection{
				Frequency: pgtype.Interval{Microseconds: 1e6, Valid: true},
				Labels:    []string{"database"},
				MaxResult: 1,
				Metrics: []store.Metric{
					{
						Name: "queries",
						Kind: store.Counter,
						Help: "total queries per database",
					},
				},
				Query: "SELECT database,queries FROM stats LIMIT $1",
				Scope: store.Node,
			},
			wantErr: "name must be specified",
		},
		{
			name:    "invalid scope",
			yaml:    invalidScope,
			wantErr: "invalid scope",
		},
		{
			name:    "unmarshal error",
			yaml:    malformed,
			wantErr: "cannot unmarshal",
		},
	}
	for _, tst := range tests {
		t.Run(tst.name, func(t *testing.T) {
			r := require.New(t)
			a := assert.New(t)
			var data []byte
			var err error
			switch {
			case tst.collection != nil:
				data, err = marshal(tst.collection)
				r.NoError(err)
			case tst.yaml != "":
				data = []byte(tst.yaml)
			default:
				panic("invalid test case")
			}
			out, err := unmarshal(data)
			if tst.wantErr != "" {
				a.ErrorContains(err, tst.wantErr)
				return
			}
			r.NoError(err)
			a.Equal(tst.collection, out)
		})
	}
}
