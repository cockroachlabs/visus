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

package collector

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cockroachdb/field-eng-powertools/stopper"
	"github.com/cockroachlabs/visus/internal/database"
	"github.com/cockroachlabs/visus/internal/server"
	"github.com/cockroachlabs/visus/internal/store"
	"github.com/go-co-op/gocron"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/pashagolub/pgxmock/v3"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	dbQuery    = "SELECT database, count FROM databases LIMIT $1"
	statsQuery = "SELECT statement, count FROM statements LIMIT $1"
)

func TestRefreshCollectors(t *testing.T) {
	r := require.New(t)
	a := assert.New(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	stop := stopper.WithContext(ctx)
	conn := &mockDB{}
	mockStore := &store.Memory{}
	mockStore.Init(ctx)
	cfg := &server.Config{}
	registry := prometheus.NewRegistry()
	server := &serverImpl{
		config:    cfg,
		conn:      conn,
		store:     mockStore,
		scheduler: gocron.NewScheduler(time.UTC),
		registry:  registry,
	}
	server.mu.stopped = true
	server.mu.scheduledJobs = make(map[string]*scheduledJob)
	server.scheduler.StartAsync()
	// Adding a collection
	dbTime := time.Now()
	dbColl := &store.Collection{
		Enabled: true,
		Frequency: pgtype.Interval{
			Microseconds: 1e6,
			Valid:        true,
		},
		Labels: []string{"database"},
		LastModified: pgtype.Timestamp{
			Time:  dbTime,
			Valid: true,
		},
		MaxResult: 1,
		Metrics: []store.Metric{
			{
				Name: "count",
				Kind: store.Counter,
				Help: "num of databases",
			},
		},
		Name:  "databases",
		Query: dbQuery,
	}
	err := mockStore.PutCollection(ctx, dbColl)
	r.NoError(err)
	err = server.Refresh(stop)
	r.NoError(err)
	coll, ok := server.getJob(dbColl.Name)
	r.True(ok)
	a.Equal(coll.collector.GetLastModified(), dbTime)
	a.Equal(1, len(server.scheduler.Jobs()))
	// Modify the collection
	dbTime = time.Now()
	dbColl.LastModified.Time = dbTime
	err = mockStore.PutCollection(ctx, dbColl)
	r.NoError(err)
	err = server.Refresh(stop)
	r.NoError(err)
	coll, ok = server.getJob(dbColl.Name)
	r.True(ok)
	a.Equal(coll.collector.GetLastModified(), dbTime)
	a.Equal(1, len(server.scheduler.Jobs()))
	// Add a new collection
	sqlTime := time.Now()
	sqlColl := &store.Collection{
		Enabled: true,
		Frequency: pgtype.Interval{
			Microseconds: 1e6,
			Valid:        true,
		},
		Labels: []string{"statement"},
		LastModified: pgtype.Timestamp{
			Time:  sqlTime,
			Valid: true,
		},
		MaxResult: 1,
		Metrics: []store.Metric{
			{
				Name: "count",
				Kind: store.Counter,
				Help: "num of statements",
			},
		},
		Name:  "statements",
		Query: statsQuery,
	}
	err = mockStore.PutCollection(ctx, sqlColl)
	r.NoError(err)
	err = server.Refresh(stop)
	r.NoError(err)
	coll, ok = server.getJob(dbColl.Name)
	r.True(ok)
	a.Equal(coll.collector.GetLastModified(), dbTime)
	sqlJob, ok := server.getJob(sqlColl.Name)
	r.True(ok)
	a.Equal(sqlJob.collector.GetLastModified(), sqlTime)
	a.Equal(2, len(server.scheduler.Jobs()))
	// Sleep just few seconds, make sure we see metrics
	time.Sleep(2 * time.Second)
	metrics, err := registry.Gather()
	r.NoError(err)
	a.Equal(2, len(metrics))
	for _, m := range metrics {
		a.Contains([]string{"databases_count", "statements_count"}, *m.Name)
	}

	// Remove the first collection
	err = mockStore.DeleteCollection(ctx, dbColl.Name)
	r.NoError(err)
	err = server.Refresh(stop)
	r.NoError(err)
	coll, ok = server.getJob(dbColl.Name)
	r.False(ok)
	a.Nil(coll)
	sqlJob, ok = server.getJob(sqlColl.Name)
	r.True(ok)
	a.Equal(sqlJob.collector.GetLastModified(), sqlTime)
	a.Equal(1, len(server.scheduler.Jobs()))

}

type mockDB struct {
	dbcount, statscount atomic.Int32
}

var _ database.Connection = &mockDB{}

// Begin implements database.Connection.
func (m *mockDB) Begin(ctx context.Context) (pgx.Tx, error) {
	panic("unimplemented")
}

// Exec implements database.Connection.
func (m *mockDB) Exec(
	ctx context.Context, sql string, arguments ...interface{},
) (pgconn.CommandTag, error) {
	panic("unimplemented")
}

// Query implements database.Connection.
func (m *mockDB) Query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error) {
	conn, err := pgxmock.NewConn()
	if err != nil {
		return nil, err
	}
	query := conn.ExpectQuery(strings.Replace(sql, "LIMIT $1", ".+", 1)).WithArgs(1)
	switch sql {
	case dbQuery:
		m.dbcount.Add(1)
		res := conn.NewRows([]string{"database", "count"})
		res.AddRow("test", int(m.dbcount.Load()))
		query.WillReturnRows(res)
	case statsQuery:
		m.statscount.Add(1)
		res := conn.NewRows([]string{"statement", "count"})
		res.AddRow("st1", int(m.statscount.Load()))
		query.WillReturnRows(res)
	}
	return conn.Query(ctx, sql, args...)
}

// QueryRow implements database.Connection.
func (m *mockDB) QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row {
	panic("unimplemented")
}
