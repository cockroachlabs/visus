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

//go:build integration

package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/cockroachlabs/visus/internal/store"
	"github.com/cockroachlabs/visus/internal/testutil"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testCollection returns a minimal, valid collection configuration for the
// given name.
func testCollection(name string) *store.Collection {
	return &store.Collection{
		Enabled:   true,
		Frequency: pgtype.Interval{Microseconds: 100000, Valid: true},
		Labels:    []string{"test"},
		MaxResult: 10,
		Metrics:   []store.Metric{},
		Name:      name,
		Query:     "SELECT * FROM test limit %1",
		Scope:     store.Node,
	}
}

// TestGetCollectionNames verifies that the list of collection names tracks
// the collections that have been put into the store.
func TestGetCollectionNames(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	r := require.New(t)
	st, _ := testutil.NewStore(ctx, t)

	names, err := st.GetCollectionNames(ctx)
	r.NoError(err)
	assert.Empty(t, names)

	r.NoError(st.PutCollection(ctx, testCollection("test1")))
	names, err = st.GetCollectionNames(ctx)
	r.NoError(err)
	assert.Equal(t, []string{"test1"}, names)

	r.NoError(st.PutCollection(ctx, testCollection("test2")))
	names, err = st.GetCollectionNames(ctx)
	r.NoError(err)
	assert.ElementsMatch(t, []string{"test1", "test2"}, names)
}

// TestDeleteCollection verifies that deleting a collection removes it, and
// its metrics, from the store.
func TestDeleteCollection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	r := require.New(t)
	st, _ := testutil.NewStore(ctx, t)

	coll := testCollection("test")
	coll.Metrics = []store.Metric{{Name: "metric1", Kind: store.Counter, Help: "help"}}
	r.NoError(st.PutCollection(ctx, coll))

	r.NoError(st.DeleteCollection(ctx, "test"))

	got, err := st.GetCollection(ctx, "test")
	r.NoError(err)
	assert.Nil(t, got)
	metrics, err := st.GetMetrics(ctx, "test")
	r.NoError(err)
	assert.Empty(t, metrics)
}

// TestGetCollection verifies the GetCollection and, indirectly, the GetMetrics functions.
func TestGetCollection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	r := require.New(t)
	st, _ := testutil.NewStore(ctx, t)

	got, err := st.GetCollection(ctx, "none")
	r.NoError(err)
	assert.Nil(t, got)

	noMetrics := testCollection("no_metrics")
	withMetrics := testCollection("with_metrics")
	withMetrics.Labels = []string{"test2"}
	withMetrics.Metrics = []store.Metric{
		{Name: "metric1", Kind: store.Counter, Help: "metric1 is a counter"},
		{Name: "metric2", Kind: store.Gauge, Help: "metric2 is a gauge"},
	}

	for _, want := range []*store.Collection{noMetrics, withMetrics} {
		r.NoError(st.PutCollection(ctx, want))

		got, err := st.GetCollection(ctx, want.Name)
		r.NoError(err)
		r.NotNil(got)
		assert.WithinDuration(t, time.Now(), got.LastModified.Time, time.Minute)
		got.LastModified = want.LastModified
		assert.Equal(t, want, got)
	}
}

// TestPutCollection verifies that a collection can be inserted, and that
// putting it again with different metrics replaces the previous ones.
func TestPutCollection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	r := require.New(t)
	st, _ := testutil.NewStore(ctx, t)

	coll := testCollection("with_metrics")
	coll.Metrics = []store.Metric{
		{Name: "metric1", Kind: store.Counter, Help: "metric1 is a counter"},
		{Name: "metric2", Kind: store.Gauge, Help: "metric2 is a gauge"},
	}
	r.NoError(st.PutCollection(ctx, coll))

	got, err := st.GetCollection(ctx, coll.Name)
	r.NoError(err)
	r.NotNil(got)
	got.LastModified = coll.LastModified
	assert.Equal(t, coll, got)

	// Putting the collection again with fewer metrics must replace, not
	// append to, the previous set.
	coll.Metrics = []store.Metric{{Name: "metric1", Kind: store.Counter, Help: "metric1 is a counter"}}
	r.NoError(st.PutCollection(ctx, coll))

	got, err = st.GetCollection(ctx, coll.Name)
	r.NoError(err)
	r.NotNil(got)
	got.LastModified = coll.LastModified
	assert.Equal(t, coll, got)
}
