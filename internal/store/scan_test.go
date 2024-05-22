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

package store

import (
	"context"
	_ "embed"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/pashagolub/pgxmock/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGetScanNames verifies we can get the list of scans in the database.
func TestGetScanNames(t *testing.T) {
	mock, err := pgxmock.NewConn()
	require.NoError(t, err)
	store := New(mock)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	columns := []string{"name"}
	tests := [][]string{
		{},
		{"test1", "test2"},
		{"test1", "test2", "test3"},
	}
	for _, tt := range tests {
		query := mock.ExpectQuery(`select "name" from _visus.scan.+`)
		res := mock.NewRows(columns)
		for _, row := range tt {
			res.AddRow(row)
		}
		query.WillReturnRows(res)
		names, err := store.GetScanNames(ctx)
		require.NoError(t, err)
		assert.Equal(t, names, tt)
	}
}

// TestDeleteScan verifies we can get the delete a scan from the database.
func TestDeleteScan(t *testing.T) {
	mock, err := pgxmock.NewConn()
	require.NoError(t, err)
	store := New(mock)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	mock.ExpectBegin()
	mock.ExpectExec("delete from _visus.pattern where scan = .+").WithArgs("test").
		WillReturnResult(pgxmock.NewResult("DELETE", 0))
	mock.ExpectExec("delete from _visus.scan where name = .+").WithArgs("test").
		WillReturnResult(pgxmock.NewResult("DELETE", 1))
	mock.ExpectCommit()
	err = store.DeleteScan(ctx, "test")
	require.NoError(t, err)
}

// TestGetScan verifies we can get a scan definition from the database.
func TestGetScan(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	type test struct {
		name      string
		scan      *Scan
		wantError bool
	}

	tests := []test{
		{"none", nil, false},
		{"no_patterns", &Scan{
			Enabled:      true,
			Format:       CRDBV2,
			Path:         "/tmp/test.log",
			LastModified: pgtype.Timestamp{Time: time.Now()},
			Name:         "no_patterns",
			Patterns:     []Pattern{},
		}, false},
		{"with_patterns", &Scan{
			Enabled:      true,
			Format:       CRDBV2,
			Path:         "/tmp/test.log",
			LastModified: pgtype.Timestamp{Time: time.Now()},
			Patterns: []Pattern{
				{"cdc", "cdc", "cdc events"},
				{"kv", "kv", "kv events"},
			},
			Name: "with_patterns",
		}, false},
	}
	for _, tt := range tests {
		mock, err := pgxmock.NewConn()
		store := New(mock)
		require.NoError(t, err)
		collQuery := mock.ExpectQuery(`select name, path, format, updated, "enabled" from _visus.scan where name = .+`).
			WithArgs(tt.name)
		res := mock.NewRows([]string{"name", "path", "format", "updated", "enabled"})
		if tt.scan != nil {
			res.AddRow(
				tt.name, tt.scan.Path, tt.scan.Format, tt.scan.LastModified, tt.scan.Enabled,
			)
		}
		collQuery.WillReturnRows(res)
		metricQuery := mock.ExpectQuery("select metric, regex, help from _visus.pattern where scan = .+").
			WithArgs(tt.name)
		res = mock.NewRows([]string{"metric", "regex", "help"})
		if tt.scan != nil {
			for _, row := range tt.scan.Patterns {
				res.AddRow(row.Name, row.Regex, row.Help)
			}
		}
		metricQuery.WillReturnRows(res)
		coll, err := store.GetScan(ctx, tt.name)
		require.NoError(t, err)
		assert.Equal(t, tt.scan, coll)
		mock.Close(ctx)
	}
}

// TestPutScan verifies we can upload a scan definition to the database.
func TestPutScan(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	type test struct {
		name      string
		scan      *Scan
		wantError bool
	}
	tests := []test{
		{"no_patterns", &Scan{
			Enabled:      true,
			Format:       CRDBV2,
			Path:         "/tmp/test.log",
			LastModified: pgtype.Timestamp{Time: time.Now()},
			Name:         "no_patterns",
			Patterns:     []Pattern{},
		}, false},
		{"with_patterns", &Scan{
			Enabled:      true,
			Format:       CRDBV2,
			Path:         "/tmp/test.log",
			LastModified: pgtype.Timestamp{Time: time.Now()},
			Patterns: []Pattern{
				{"cdc", "cdc", "cdc events"},
				{"kv", "kv", "kv events"},
			},
			Name: "with_patterns",
		}, false},
	}
	for _, tt := range tests {
		mock, err := pgxmock.NewConn()
		store := New(mock)
		require.NoError(t, err)
		mock.ExpectBegin()
		mock.ExpectExec("delete from _visus.pattern where scan = .+").WithArgs(tt.scan.Name).
			WillReturnResult(pgxmock.NewResult("DELETE", 0))
		mock.ExpectExec(
			`UPSERT INTO _visus.scan \(name, path, format, enabled, updated\) VALUES .+`).
			WithArgs(tt.scan.Name, tt.scan.Path, tt.scan.Format, tt.scan.Enabled).
			WillReturnResult(pgxmock.NewResult("UPDATE", 1))
		for _, pattern := range tt.scan.Patterns {
			mock.ExpectExec(`INSERT INTO _visus.pattern \(scan,metric,regex,help\) VALUES .+`).
				WithArgs(tt.scan.Name, pattern.Name, pattern.Regex, pattern.Help).
				WillReturnResult(pgxmock.NewResult("INSERT", 1))
		}
		mock.ExpectCommit()
		err = store.PutScan(ctx, tt.scan)
		require.NoError(t, err)
		mock.Close(ctx)
	}
}
