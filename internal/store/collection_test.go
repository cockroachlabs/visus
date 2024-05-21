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

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/pashagolub/pgxmock/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetCollectionNames(t *testing.T) {
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
		query := mock.ExpectQuery("select name from _visus.collection.+")
		res := mock.NewRows(columns)
		for _, row := range tt {
			res.AddRow(row)
		}
		query.WillReturnRows(res)
		names, err := store.GetCollectionNames(ctx)
		require.NoError(t, err)
		assert.Equal(t, names, tt)
	}
}

func TestDeleteCollection(t *testing.T) {
	mock, err := pgxmock.NewConn()
	require.NoError(t, err)
	store := New(mock)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	mock.ExpectBegin()
	mock.ExpectExec("delete from _visus.metric where collection = .+").WithArgs("test").
		WillReturnResult(pgxmock.NewResult("DELETE", 0))
	mock.ExpectExec("delete from _visus.collection where name = .+").WithArgs("test").
		WillReturnResult(pgxmock.NewResult("DELETE", 1))
	mock.ExpectCommit()
	err = store.DeleteCollection(ctx, "test")
	require.NoError(t, err)
}

// TestGetCollection verifies the GetCollection and, indirectly, the GetMetrics functions.
func TestGetCollection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	type test struct {
		name       string
		collection *Collection
		wantError  bool
	}

	tests := []test{
		{"none", nil, false},
		{"no_metrics", &Collection{
			Enabled:      true,
			Frequency:    pgtype.Interval{Microseconds: 100000},
			Labels:       []string{"test"},
			LastModified: pgtype.Timestamp{Time: time.Now()},
			MaxResult:    10,
			Metrics:      []Metric{},
			Name:         "no_metrics",
			Query:        "SELECT * FROM test limit %1",
			Scope:        Node,
		}, false},
		{"with_metrics", &Collection{
			Enabled:      true,
			Frequency:    pgtype.Interval{Microseconds: 100000},
			Labels:       []string{"test2"},
			LastModified: pgtype.Timestamp{Time: time.Now()},
			MaxResult:    10,
			Metrics: []Metric{
				{"metric1", Counter, "metric1 is a counter"},
				{"metric2", Gauge, "metric2 is a gauge"},
			},
			Name:  "with_metrics",
			Query: "SELECT * FROM test limit %1",
			Scope: Node,
		}, false},
	}
	for _, tt := range tests {
		mock, err := pgxmock.NewConn()
		store := New(mock)
		require.NoError(t, err)
		collQuery := mock.ExpectQuery("select name, updated, enabled, scope, maxResults, frequency, query, labels from _visus.collection where name = .+").
			WithArgs(tt.name)
		res := mock.NewRows([]string{"name", "updated", "enabled", "scope", "maxResults", "frequency", "query", "labels"})
		if tt.collection != nil {
			res.AddRow(
				tt.name, tt.collection.LastModified,
				tt.collection.Enabled, tt.collection.Scope,
				tt.collection.MaxResult, tt.collection.Frequency,
				tt.collection.Query, tt.collection.Labels,
			)
		}
		collQuery.WillReturnRows(res)
		metricQuery := mock.ExpectQuery("select metric,kind, help from _visus.metric where collection = .+").
			WithArgs(tt.name)
		res = mock.NewRows([]string{"metric", "kind", "help"})
		if tt.collection != nil {
			for _, row := range tt.collection.Metrics {
				res.AddRow(row.Name, row.Kind, row.Help)
			}
		}
		metricQuery.WillReturnRows(res)
		coll, err := store.GetCollection(ctx, tt.name)
		require.NoError(t, err)
		assert.Equal(t, tt.collection, coll)
		mock.Close(ctx)
	}
}

func TestPutCollection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	type test struct {
		name       string
		collection *Collection
		wantError  bool
	}

	tests := []test{
		{"no_metrics", &Collection{
			Enabled:      true,
			Frequency:    pgtype.Interval{Microseconds: 100000},
			Labels:       []string{"test"},
			LastModified: pgtype.Timestamp{Time: time.Now()},
			MaxResult:    10,
			Metrics:      []Metric{},
			Name:         "no_metrics",
			Query:        "SELECT * FROM test limit %1",
			Scope:        Node,
		}, false},
		{"with_metrics", &Collection{
			Enabled:      true,
			Frequency:    pgtype.Interval{Microseconds: 100000},
			Labels:       []string{"test2"},
			LastModified: pgtype.Timestamp{Time: time.Now()},
			MaxResult:    10,
			Metrics: []Metric{
				{"metric1", Counter, "metric1 is a counter"},
				{"metric2", Gauge, "metric2 is a gauge"},
			},
			Name:  "with_metrics",
			Query: "SELECT * FROM test limit %1",
			Scope: Node,
		}, false},
	}
	for _, tt := range tests {
		mock, err := pgxmock.NewConn()
		store := New(mock)
		require.NoError(t, err)
		mock.ExpectBegin()
		mock.ExpectExec("delete from _visus.metric where collection = .+").WithArgs(tt.collection.Name).
			WillReturnResult(pgxmock.NewResult("DELETE", 0))
		mock.ExpectExec(
			`UPSERT INTO _visus.collection \(name, enabled, scope, maxResults, frequency, query, labels, updated\) VALUES .+`).
			WithArgs(tt.collection.Name, tt.collection.Enabled, tt.collection.Scope,
				tt.collection.MaxResult, tt.collection.Frequency, tt.collection.Query,
				tt.collection.Labels).
			WillReturnResult(pgxmock.NewResult("UPDATE", 1))
		for _, metric := range tt.collection.Metrics {
			mock.ExpectExec(`INSERT INTO _visus.metric \(collection,metric,kind,help\) VALUES .+`).
				WithArgs(tt.collection.Name, metric.Name, metric.Kind, metric.Help).
				WillReturnResult(pgxmock.NewResult("INSERT", 1))
		}
		mock.ExpectCommit()
		err = store.PutCollection(ctx, tt.collection)
		require.NoError(t, err)
		mock.Close(ctx)
	}
}
