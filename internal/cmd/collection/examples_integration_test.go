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

//go:build integration

package collection

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cockroachlabs/visus/internal/collector"
	"github.com/cockroachlabs/visus/internal/database"
	"github.com/cockroachlabs/visus/internal/testutil"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

// exampleCase describes how to exercise one examples/*.yaml collection
// against a real cluster.
type exampleCase struct {
	// file is the YAML file name under examples/.
	file string
	// setup prepares the database state the query needs. admin is a
	// read-write connection to a fresh scratch database; ro allows
	// crdb_internal/system access (see unsafeInternalsPool).
	setup func(ctx context.Context, t *testing.T, admin, ro database.Connection)
	// requireData requires at least one non-NULL metric sample. When false,
	// the query only needs to run without a SQL or column-count error.
	requireData bool
}

// exampleCases lists every examples/*.yaml file that is actually a
// collection. latency.yaml (a histogram config, no query field) and
// dashboard.json (a Grafana export) are omitted since they'd fail unmarshal.
var exampleCases = []exampleCase{
	{file: "blocking_statements.yaml", setup: setupContention, requireData: true},
	{file: "contended_tables.yaml", setup: setupContention, requireData: true},
	{file: "changefeed_health.yaml", setup: setupChangefeed, requireData: true},
	// system.protected_ts_records is normally populated by a paused job;
	// not verified live yet, so just check the query runs.
	{file: "protected_ts.yaml", setup: setupPlainTable, requireData: false},
	// On CockroachDB v26.3+, write buffering can keep an open write from
	// showing up as a replicated intent, so only check the query runs.
	{file: "intents.yaml", setup: setupPlainTable, requireData: false},
	{file: "inflight.yaml", setup: setupInflightQuery, requireData: true},
	{file: "index_usage.yaml", setup: setupPlainTable, requireData: true},
	{file: "tables.yaml", setup: setupPlainTable, requireData: true},
	// estimated_row_count is populated by an async stats job that stayed
	// NULL for 6+ seconds in testing, so only check the query runs.
	{file: "tables_rows.yaml", setup: setupPlainTable, requireData: false},
	// garbage_percent > 0 AND total_bytes > 1MiB needs several MiB of real
	// MVCC garbage; too expensive for now, so only check the query runs.
	{file: "table_mvcc.yaml", setup: setupPlainTable, requireData: false},
	{file: "table_mvcc_node.yaml", setup: setupPlainTable, requireData: false},
	{file: "sqlactivity.yaml", setup: setupNodeStatementStats, requireData: true},
	{file: "sqlefficiency.yaml", setup: setupNodeStatementStats, requireData: true},
	{file: "cluster_sqlactivity.yaml", setup: setupPersistedStatementStats, requireData: true},
}

// statsFlushOnce lowers sql.stats.flush.interval once per run, since
// cluster_sqlactivity.yaml needs the persisted stats table (default flush:
// 10 minutes), unlike sqlactivity.yaml/sqlefficiency.yaml's in-memory stats.
var statsFlushOnce sync.Once

// contentionResolutionOnce lowers sql.contention.event_store.resolution_interval
// (default 30s) once per run so setupContention's events resolve quickly.
var contentionResolutionOnce sync.Once

// TestExamples runs every collection in examples/ against a real
// CockroachDB cluster, catching query/schema drift that mocked-rows tests
// (collector_test.go) can't.
func TestExamples(t *testing.T) {
	for _, tc := range exampleCases {
		t.Run(tc.file, func(t *testing.T) {
			runExampleCase(t, tc)
		})
	}
}

func runExampleCase(t *testing.T, tc exampleCase) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	r := require.New(t)

	data, err := os.ReadFile(filepath.Join(examplesDir(t), tc.file))
	r.NoError(err)
	coll, err := unmarshal(data)
	r.NoError(err)

	admin, dbName := testutil.NewDatabase(ctx, t)
	ro, err := unsafeInternalsPool(ctx, sameDBURL(dbName), testutil.SupportsAllowUnsafeInternals())
	r.NoError(err)
	defer ro.Close()

	tc.setup(ctx, t, admin, ro)

	registry := prometheus.NewRegistry()
	c, err := collector.FromCollection(coll, registry)
	r.NoError(err)
	r.NoError(c.Collect(ctx, ro))

	mfs, err := registry.Gather()
	r.NoError(err)
	if tc.requireData {
		r.True(hasSamples(mfs, coll.Name),
			"expected at least one sample for collection %q, got %+v", coll.Name, mfs)
	}
}

// examplesDir locates the repo's examples/ directory relative to this
// source file, so the test finds it regardless of the working directory
// "go test" is invoked from.
func examplesDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller failed")
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "examples")
}

// sameDBURL returns the shared cluster's connection URL pointed at dbName,
// mirroring how testutil.NewDatabase builds its own connection.
func sameDBURL(dbName string) string {
	u := testutil.PGURL()
	u.Path = "/" + dbName
	return u.String()
}

// unsafeInternalsPool opens a pool like database.ReadOnly, but skips its
// follower-reads setting: follower reads are a few seconds stale by design,
// which would make a query against a scratch database created milliseconds
// earlier fail with a confusing "database does not exist" instead of
// actually running.
//
// If allowUnsafeInternals is true, it also sets allow_unsafe_internals on
// the connection, needed on CockroachDB v25.1+ to query crdb_internal (see
// testutil.SupportsAllowUnsafeInternals). Older series never gated
// crdb_internal access behind the setting, so there's nothing to enable.
func unsafeInternalsPool(
	ctx context.Context, url string, allowUnsafeInternals bool,
) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	if allowUnsafeInternals {
		cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
			_, err := conn.Exec(ctx, "set allow_unsafe_internals = true;")
			return err
		}
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

// pollUntil polls cond until it returns true or timeout elapses. Unlike
// require/assert.Eventually, it doesn't leak a goroutine past a timeout.
func pollUntil(timeout, interval time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for {
		if cond() {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(interval)
	}
}

// hasSamples reports whether any metric family for the named collection has
// at least one sample, mirroring testCmd's own name-prefix filter.
func hasSamples(mfs []*dto.MetricFamily, collectionName string) bool {
	prefix := collectionName + "_"
	for _, mf := range mfs {
		if strings.HasPrefix(mf.GetName(), prefix) && len(mf.GetMetric()) > 0 {
			return true
		}
	}
	return false
}

// setupPlainTable creates a single scratch table with one row, enough for
// collections that just need any user table to exist (e.g. tables.yaml
// enumerates crdb_internal.tables cluster-wide).
func setupPlainTable(ctx context.Context, t *testing.T, admin, _ database.Connection) {
	t.Helper()
	r := require.New(t)
	_, err := admin.Exec(ctx, "CREATE TABLE t (id INT PRIMARY KEY, v STRING)")
	r.NoError(err)
	_, err = admin.Exec(ctx, "INSERT INTO t VALUES (1, 'a')")
	r.NoError(err)
}

// setupContention races two transactions over the same row, which
// CockroachDB records as a transaction contention event
// (blocking_statements.yaml and contended_tables.yaml both read from it).
// admin is pgxpool-backed, so the holder and the blocked statement get
// distinct physical connections automatically.
//
// The first contention event after cluster start always has an all-zero
// blocking_txn_fingerprint_id (unresolved), which blocking_statements.yaml
// filters out, so this retries until a resolved (non-zero) one appears.
func setupContention(ctx context.Context, t *testing.T, admin, ro database.Connection) {
	t.Helper()
	r := require.New(t)

	contentionResolutionOnce.Do(func() {
		_, err := admin.Exec(ctx, "SET CLUSTER SETTING sql.contention.event_store.resolution_interval = '1s'")
		r.NoError(err)
	})

	_, err := admin.Exec(ctx, "CREATE TABLE c (id INT PRIMARY KEY, v INT)")
	r.NoError(err)
	_, err = admin.Exec(ctx, "INSERT INTO c VALUES (1, 0)")
	r.NoError(err)

	for attempt := 0; attempt < 10; attempt++ {
		tx, err := admin.Begin(ctx)
		r.NoError(err)
		_, err = tx.Exec(ctx, "UPDATE c SET v = v + 1 WHERE id = 1")
		r.NoError(err)

		blocked := make(chan error, 1)
		go func() {
			_, err := admin.Exec(ctx, "UPDATE c SET v = v + 1 WHERE id = 1")
			blocked <- err
		}()
		time.Sleep(300 * time.Millisecond) // let the blocked statement start waiting
		r.NoError(tx.Commit(ctx))
		r.NoError(<-blocked)

		found := pollUntil(3*time.Second, 200*time.Millisecond, func() bool {
			var n int
			err := ro.QueryRow(ctx,
				"SELECT count(*) FROM crdb_internal.transaction_contention_events "+
					"WHERE encode(blocking_txn_fingerprint_id, 'hex') != '0000000000000000'").Scan(&n)
			return err == nil && n > 0
		})
		if found {
			return
		}
	}
	t.Fatal("expected a transaction contention event with a resolved blocking fingerprint")
}

// setupChangefeed creates a real running changefeed job, which
// changefeed_health.yaml reads via [SHOW CHANGEFEED JOBS].
func setupChangefeed(ctx context.Context, t *testing.T, admin, ro database.Connection) {
	t.Helper()
	r := require.New(t)

	// testserver clusters start with rangefeeds disabled.
	_, err := admin.Exec(ctx, "SET CLUSTER SETTING kv.rangefeed.enabled = true")
	r.NoError(err)

	_, err = admin.Exec(ctx, "CREATE TABLE cf (id INT PRIMARY KEY)")
	r.NoError(err)

	var jobID int64
	err = admin.QueryRow(ctx,
		"CREATE CHANGEFEED FOR TABLE cf INTO 'nodelocal://1/cf'").Scan(&jobID)
	r.NoError(err)

	r.True(pollUntil(5*time.Second, 200*time.Millisecond, func() bool {
		var n int
		err := ro.QueryRow(ctx,
			"SELECT count(*) FROM [SHOW CHANGEFEED JOBS] WHERE job_id = $1 AND status = 'running'",
			jobID).Scan(&n)
		return err == nil && n > 0
	}), "expected the changefeed job to be running")
}

// setupInflightQuery starts a long-running query on a separate connection
// so it's still executing (visible in crdb_internal.node_queries) when
// inflight.yaml's Collect call runs.
func setupInflightQuery(ctx context.Context, t *testing.T, admin, _ database.Connection) {
	t.Helper()
	go func() {
		_, _ = admin.Exec(ctx, "SELECT pg_sleep(3)")
	}()
	time.Sleep(300 * time.Millisecond) // let the background query start executing
}

// setupNodeStatementStats runs a representative query so it shows up in
// the in-memory crdb_internal.node_statement_statistics, which
// sqlactivity.yaml and sqlefficiency.yaml read directly (no flush delay).
func setupNodeStatementStats(ctx context.Context, t *testing.T, admin, ro database.Connection) {
	t.Helper()
	r := require.New(t)
	_, err := admin.Exec(ctx, "CREATE TABLE s (id INT PRIMARY KEY, v STRING)")
	r.NoError(err)
	_, err = admin.Exec(ctx, "INSERT INTO s VALUES (1, 'a')")
	r.NoError(err)
	_, err = admin.Exec(ctx, "SELECT v FROM s WHERE id = 1")
	r.NoError(err)

	r.True(pollUntil(5*time.Second, 200*time.Millisecond, func() bool {
		var n int
		err := ro.QueryRow(ctx,
			"SELECT count(*) FROM crdb_internal.node_statement_statistics "+
				"WHERE application_name NOT LIKE '$ internal-%'").Scan(&n)
		return err == nil && n > 0
	}), "expected node statement statistics for the test query")
}

// setupPersistedStatementStats waits for a query to reach the persisted
// system.statement_statistics table, which cluster_sqlactivity.yaml needs
// for its max(aggregated_ts) subquery. Polling crdb_internal.statement_statistics
// instead would race ahead of the actual flush, since it can reflect
// not-yet-flushed in-memory stats.
func setupPersistedStatementStats(
	ctx context.Context, t *testing.T, admin, ro database.Connection,
) {
	t.Helper()
	r := require.New(t)

	statsFlushOnce.Do(func() {
		_, err := admin.Exec(ctx, "SET CLUSTER SETTING sql.stats.flush.interval = '2s'")
		r.NoError(err)
	})

	_, err := admin.Exec(ctx, "CREATE TABLE p (id INT PRIMARY KEY, v STRING)")
	r.NoError(err)
	_, err = admin.Exec(ctx, "INSERT INTO p VALUES (1, 'a')")
	r.NoError(err)

	var before int
	r.NoError(ro.QueryRow(ctx,
		"SELECT count(*) FROM system.statement_statistics").Scan(&before))

	_, err = admin.Exec(ctx, "SELECT v FROM p WHERE id = 1")
	r.NoError(err)

	r.True(pollUntil(15*time.Second, 500*time.Millisecond, func() bool {
		var after int
		err := ro.QueryRow(ctx,
			"SELECT count(*) FROM system.statement_statistics").Scan(&after)
		return err == nil && after > before
	}), "expected new persisted statement statistics")
}
