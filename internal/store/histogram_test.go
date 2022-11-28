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

package store

import (
	"context"
	_ "embed"
	"testing"
	"time"

	"github.com/jackc/pgtype"
	"github.com/pashagolub/pgxmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetHistogramNames(t *testing.T) {
	mock, err := pgxmock.NewConn()
	require.NoError(t, err)
	store := New(mock)
	ctx, _ := context.WithTimeout(context.Background(), 1*time.Second)
	columns := []string{"name"}
	tests := [][]string{
		{},
		{"test1", "test2"},
		{"test1", "test2", "test3"},
	}
	for _, tt := range tests {
		query := mock.ExpectQuery(`select "name" from _visus.histogram`)
		res := mock.NewRows(columns)
		for _, row := range tt {
			res.AddRow(row)
		}
		query.WillReturnRows(res)
		names, err := store.GetHistogramNames(ctx)
		require.NoError(t, err)
		assert.Equal(t, names, tt)
	}
}

func TestGetHistogram(t *testing.T) {
	ctx, _ := context.WithTimeout(context.Background(), 1*time.Second)
	type test struct {
		name      string
		histogram *Histogram
		wantError bool
	}

	tests := []test{
		{"none", nil, false},
		{"sql_latency", &Histogram{
			Enabled:      true,
			Name:         "sql_latency",
			Bins:         10,
			Start:        10,
			End:          1000,
			Regex:        "sql_latency$",
			LastModified: pgtype.Timestamp{Time: time.Now()},
		}, false,
		},
	}
	for _, tt := range tests {
		mock, err := pgxmock.NewConn()
		store := New(mock)
		require.NoError(t, err)
		histQuery := mock.ExpectQuery("select name, regex, updated, \"enabled\", bins, \"start\", \"end\" from _visus.histogram where name = .+").
			WithArgs(tt.name)
		res := mock.NewRows([]string{"name", "regex", "updated", "enabled", "bins", "start", "end"})
		if tt.histogram != nil {
			res.AddRow(
				tt.name, tt.histogram.Regex, tt.histogram.LastModified,
				tt.histogram.Enabled, tt.histogram.Bins,
				tt.histogram.Start, tt.histogram.End,
			)
		}
		histQuery.WillReturnRows(res)
		hist, err := store.GetHistogram(ctx, tt.name)
		require.NoError(t, err)
		assert.Equal(t, tt.histogram, hist)
		mock.Close(ctx)
	}
}

func TestDeleteHistogram(t *testing.T) {
	mock, err := pgxmock.NewConn()
	require.NoError(t, err)
	store := New(mock)
	ctx, _ := context.WithTimeout(context.Background(), 1*time.Second)
	mock.ExpectBegin()
	mock.ExpectExec("delete from _visus.histogram where name = .+").WithArgs("test").
		WillReturnResult(pgxmock.NewResult("DELETE", 1))
	err = store.DeleteHistogram(ctx, "test")
	require.NoError(t, err)
}

func TestPutHistogram(t *testing.T) {
	ctx, _ := context.WithTimeout(context.Background(), 1*time.Second)
	type test struct {
		name      string
		histogram *Histogram
		wantError bool
	}

	tests := []test{
		{"sql_latency", &Histogram{
			Enabled:      true,
			Name:         "sql_latency",
			Bins:         10,
			Start:        10,
			End:          1000,
			Regex:        "sql_latency$",
			LastModified: pgtype.Timestamp{Time: time.Now()},
		}, false,
		},
	}
	for _, tt := range tests {
		mock, err := pgxmock.NewConn()
		store := New(mock)
		require.NoError(t, err)
		mock.ExpectBegin()

		mock.ExpectExec(
			`UPSERT INTO _visus.histogram \(name, regex, bins, "start", "end"\) VALUES .+`).
			WithArgs(tt.histogram.Name, tt.histogram.Regex, tt.histogram.Bins,
				tt.histogram.Start, tt.histogram.End,
			).WillReturnResult(pgxmock.NewResult("UPDATE", 1))

		err = store.PutHistogram(ctx, tt.histogram)
		require.NoError(t, err)
		mock.Close(ctx)
	}
}
