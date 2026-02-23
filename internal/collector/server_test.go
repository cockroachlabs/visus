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

func TestClusterScopeCollectors(t *testing.T) {
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
	srv := &serverImpl{
		config:    cfg,
		conn:      conn,
		store:     mockStore,
		scheduler: gocron.NewScheduler(time.UTC),
		registry:  registry,
	}
	srv.mu.stopped = true
	srv.mu.scheduledJobs = make(map[string]*scheduledJob)
	srv.scheduler.StartAsync()

	// Create a node-scoped collection.
	nodeColl := &store.Collection{
		Enabled: true,
		Frequency: pgtype.Interval{
			Microseconds: 1e6,
			Valid:        true,
		},
		Labels: []string{"database"},
		LastModified: pgtype.Timestamp{
			Time:  time.Now(),
			Valid: true,
		},
		MaxResult: 1,
		Metrics: []store.Metric{
			{Name: "count", Kind: store.Counter, Help: "num of databases"},
		},
		Name:  "databases",
		Query: dbQuery,
		Scope: store.Node,
	}
	// Create a cluster-scoped collection.
	clusterColl := &store.Collection{
		Enabled: true,
		Frequency: pgtype.Interval{
			Microseconds: 1e6,
			Valid:        true,
		},
		Labels: []string{"statement"},
		LastModified: pgtype.Timestamp{
			Time:  time.Now(),
			Valid: true,
		},
		MaxResult: 1,
		Metrics: []store.Metric{
			{Name: "count", Kind: store.Counter, Help: "num of statements"},
		},
		Name:  "statements",
		Query: statsQuery,
		Scope: store.Cluster,
	}
	r.NoError(mockStore.PutCollection(ctx, nodeColl))
	r.NoError(mockStore.PutCollection(ctx, clusterColl))

	// When main node: both collections should be scheduled.
	mockStore.SetMainNode(true)
	r.NoError(srv.Refresh(stop))
	_, ok := srv.getJob(nodeColl.Name)
	a.True(ok, "node-scoped collection should be scheduled")
	_, ok = srv.getJob(clusterColl.Name)
	a.True(ok, "cluster-scoped collection should be scheduled on main node")
	a.Equal(2, len(srv.scheduler.Jobs()))

	// When not main node: only node-scoped should remain.
	mockStore.SetMainNode(false)
	r.NoError(srv.Refresh(stop))
	_, ok = srv.getJob(nodeColl.Name)
	a.True(ok, "node-scoped collection should still be scheduled")
	_, ok = srv.getJob(clusterColl.Name)
	a.False(ok, "cluster-scoped collection should be removed on non-main node")
	a.Equal(1, len(srv.scheduler.Jobs()))
}

func TestMainNodeTransition(t *testing.T) {
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
	srv := &serverImpl{
		config:    cfg,
		conn:      conn,
		store:     mockStore,
		scheduler: gocron.NewScheduler(time.UTC),
		registry:  registry,
	}
	srv.mu.stopped = true
	srv.mu.scheduledJobs = make(map[string]*scheduledJob)
	srv.scheduler.StartAsync()

	// Create a cluster-scoped collection.
	clusterColl := &store.Collection{
		Enabled: true,
		Frequency: pgtype.Interval{
			Microseconds: 1e6,
			Valid:        true,
		},
		Labels: []string{"statement"},
		LastModified: pgtype.Timestamp{
			Time:  time.Now(),
			Valid: true,
		},
		MaxResult: 1,
		Metrics: []store.Metric{
			{Name: "count", Kind: store.Counter, Help: "num of statements"},
		},
		Name:  "statements",
		Query: statsQuery,
		Scope: store.Cluster,
	}
	r.NoError(mockStore.PutCollection(ctx, clusterColl))

	// Start as non-main node: cluster-scoped should not be scheduled.
	mockStore.SetMainNode(false)
	r.NoError(srv.Refresh(stop))
	_, ok := srv.getJob(clusterColl.Name)
	a.False(ok, "cluster-scoped should not be scheduled on non-main node")
	a.Equal(0, len(srv.scheduler.Jobs()))

	// Transition to main node: cluster-scoped should be added.
	mockStore.SetMainNode(true)
	r.NoError(srv.Refresh(stop))
	_, ok = srv.getJob(clusterColl.Name)
	a.True(ok, "cluster-scoped should be scheduled after becoming main node")
	a.Equal(1, len(srv.scheduler.Jobs()))

	// Transition back to non-main node: cluster-scoped should be removed.
	mockStore.SetMainNode(false)
	r.NoError(srv.Refresh(stop))
	_, ok = srv.getJob(clusterColl.Name)
	a.False(ok, "cluster-scoped should be removed after losing main node status")
	a.Equal(0, len(srv.scheduler.Jobs()))
}

type mockDB struct {
	dbcount, statscount atomic.Int32
}

var _ database.Connection = &mockDB{}

// Begin implements database.Connection.
func (m *mockDB) Begin(ctx context.Context) (pgx.Tx, error) {
	return &mockTx{
		db: m,
	}, nil
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

type mockTx struct {
	db *mockDB
}

var _ pgx.Tx = &mockTx{}

// Begin implements pgx.Tx.
func (m *mockTx) Begin(ctx context.Context) (pgx.Tx, error) {
	panic("unimplemented")
}

// Commit implements pgx.Tx.
func (m *mockTx) Commit(ctx context.Context) error {
	return nil
}

// Conn implements pgx.Tx.
func (m *mockTx) Conn() *pgx.Conn {
	panic("unimplemented")
}

// CopyFrom implements pgx.Tx.
func (m *mockTx) CopyFrom(
	ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource,
) (int64, error) {
	panic("unimplemented")
}

// Exec implements pgx.Tx.
func (m *mockTx) Exec(
	ctx context.Context, sql string, args ...any,
) (commandTag pgconn.CommandTag, err error) {
	return m.db.Exec(ctx, sql, args...)
}

// LargeObjects implements pgx.Tx.
func (m *mockTx) LargeObjects() pgx.LargeObjects {
	panic("unimplemented")
}

// Prepare implements pgx.Tx.
func (m *mockTx) Prepare(
	ctx context.Context, name string, sql string,
) (*pgconn.StatementDescription, error) {
	panic("unimplemented")
}

// Query implements pgx.Tx.
func (m *mockTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return m.db.Query(ctx, sql, args...)
}

// QueryRow implements pgx.Tx.
func (m *mockTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return m.db.QueryRow(ctx, sql, args...)
}

// Rollback implements pgx.Tx.
func (m *mockTx) Rollback(ctx context.Context) error {
	return nil
}

// SendBatch implements pgx.Tx.
func (m *mockTx) SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults {
	panic("unimplemented")
}
