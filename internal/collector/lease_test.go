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

package collector

import (
	"context"
	"net/url"
	"testing"
	"time"

	"github.com/cockroachdb/cockroach-go/v2/testserver"
	"github.com/cockroachdb/field-eng-powertools/lease"
	"github.com/cockroachdb/field-eng-powertools/stopper"
	"github.com/cockroachlabs/visus/internal/database"
	"github.com/cockroachlabs/visus/internal/server"
	"github.com/cockroachlabs/visus/internal/store"
	"github.com/go-co-op/gocron"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testSetup starts a CockroachDB test server, initializes the schema,
// and returns the pg URL.
func testSetup(t *testing.T) *url.URL {
	t.Helper()
	r := require.New(t)

	ts, err := testserver.NewTestServer()
	r.NoError(err)
	t.Cleanup(ts.Stop)

	pgURL := ts.PGURL()
	r.NotNil(pgURL)

	ctx := context.Background()
	adminConn, err := database.New(ctx, pgURL.String())
	r.NoError(err)

	st := store.New(adminConn)
	r.NoError(st.Init(ctx))

	return pgURL
}

// newTestServer creates a collector server with its own connections,
// scheduler, and registry. Returns the server and its registry.
// An optional lease.Config can be provided to override defaults.
func newTestServer(
	ctx context.Context,
	t *testing.T,
	stop *stopper.Context,
	pgURL *url.URL,
	st store.Store,
	leaseCfg ...lease.Config,
) (*serverImpl, *prometheus.Registry) {
	t.Helper()
	r := require.New(t)

	adminConn, err := database.New(ctx, pgURL.String())
	r.NoError(err)

	roConn, err := database.ReadOnly(ctx, pgURL.String(), false)
	r.NoError(err)

	cfg := &server.Config{
		Refresh: 1 * time.Second,
	}
	registry := prometheus.NewRegistry()
	scheduler := gocron.NewScheduler(time.UTC)
	scheduler.StartAsync()
	t.Cleanup(scheduler.Stop)

	srv, err := New(cfg, st, adminConn, roConn, registry, scheduler)
	r.NoError(err)

	impl := srv.(*serverImpl)
	impl.releaseLease = make(chan struct{})
	if len(leaseCfg) > 0 {
		lc := leaseCfg[0]
		lc.Conn = adminConn
		lc.Table = store.LeaseTable
		r.NoError(impl.start(stop, lc))
	} else {
		r.NoError(impl.Start(stop))
	}

	return impl, registry
}

func clusterCollection(name string) *store.Collection {
	return &store.Collection{
		Enabled: true,
		Frequency: pgtype.Interval{
			Microseconds: 1e6,
			Valid:        true,
		},
		Labels: []string{"database_name"},
		LastModified: pgtype.Timestamp{
			Time:  time.Now(),
			Valid: true,
		},
		MaxResult: 10,
		Metrics: []store.Metric{
			{
				Name: "count",
				Kind: store.Gauge,
				Help: "number of databases",
			},
		},
		Name:  name,
		Query: "SELECT database_name, count(*)::FLOAT8 AS count FROM [SHOW DATABASES] GROUP BY 1 LIMIT $1",
		Scope: store.Cluster,
	}
}

// TestLeaseLifecycle verifies lease acquisition, cluster-scoped collector
// execution, and handover when the lease holder stops.
func TestLeaseLifecycle(t *testing.T) {
	r := require.New(t)
	a := assert.New(t)

	pgURL := testSetup(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	adminConn, err := database.New(ctx, pgURL.String())
	r.NoError(err)
	st := store.New(adminConn)

	r.NoError(st.PutCollection(ctx, clusterCollection("handover_databases")))

	// Use short lease timings so the handover happens quickly.
	leaseCfg := lease.Config{
		Lifetime:   5 * time.Second,
		Poll:       1 * time.Second,
		RetryDelay: 1 * time.Second,
	}

	// Each server gets its own cancellable context and stopper.
	ctx1, cancel1 := context.WithCancel(ctx)
	defer cancel1()
	ctx2, cancel2 := context.WithCancel(ctx)
	defer cancel2()
	stop1 := stopper.WithContext(ctx1)
	stop2 := stopper.WithContext(ctx2)

	srv1, reg1 := newTestServer(ctx, t, stop1, pgURL, st, leaseCfg)
	srv2, reg2 := newTestServer(ctx, t, stop2, pgURL, st, leaseCfg)

	// Wait until one of them acquires the lease.
	r.Eventually(func() bool {
		return srv1.isLeaseHolder() || srv2.isLeaseHolder()
	}, 30*time.Second, 100*time.Millisecond, "no server acquired the lease")

	// Only one server should hold the lease at a time.
	a.False(srv1.isLeaseHolder() && srv2.isLeaseHolder(),
		"both servers hold the lease simultaneously")

	// Identify holder and survivor, then release the holder's lease.
	var holder, survivor *serverImpl
	var holderReg, survivorReg *prometheus.Registry
	if srv1.isLeaseHolder() {
		t.Log("srv1 holds the lease, releasing it")
		holder, survivor = srv1, srv2
		holderReg, survivorReg = reg1, reg2
	} else {
		t.Log("srv2 holds the lease, releasing it")
		holder, survivor = srv2, srv1
		holderReg, survivorReg = reg2, reg1
	}

	// Verify the holder produces metrics before we release the lease.
	a.Eventually(func() bool {
		return hasMetric(holderReg, "handover_databases_count")
	}, 10*time.Second, 500*time.Millisecond, "holder never produced metrics")

	close(holder.releaseLease)

	// The old holder should no longer be lease holder.
	a.Eventually(func() bool {
		return !holder.isLeaseHolder()
	}, 10*time.Second, 100*time.Millisecond, "holder lease did not expire")

	// The holder's cluster metric should be deregistered on lease loss.
	a.Eventually(func() bool {
		return !hasMetric(holderReg, "handover_databases_count")
	}, 10*time.Second, 500*time.Millisecond, "holder still has cluster metric after lease loss")

	// The survivor should acquire the lease.
	r.Eventually(func() bool {
		return survivor.isLeaseHolder()
	}, 30*time.Second, 100*time.Millisecond, "survivor never acquired the lease after handover")

	// The survivor should produce metrics.
	a.Eventually(func() bool {
		return hasMetric(survivorReg, "handover_databases_count")
	}, 10*time.Second, 500*time.Millisecond, "survivor never produced metrics after handover")
}

func hasMetric(registry *prometheus.Registry, name string) bool {
	metrics, err := registry.Gather()
	if err != nil {
		return false
	}
	for _, m := range metrics {
		if *m.Name == name {
			return true
		}
	}
	return false
}
