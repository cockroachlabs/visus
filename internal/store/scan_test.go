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

//go:build integration

package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/cockroachlabs/visus/internal/store"
	"github.com/cockroachlabs/visus/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testScan returns a minimal, valid scan configuration for the given name.
func testScan(name string) *store.Scan {
	return &store.Scan{
		Enabled:  true,
		Format:   store.CRDBv2,
		Path:     "/tmp/test.log",
		Name:     name,
		Patterns: []store.Pattern{},
	}
}

// TestGetScanNames verifies that the list of scan names tracks the scans
// that have been put into the store.
func TestGetScanNames(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	r := require.New(t)
	st, _ := testutil.NewStore(ctx, t)

	names, err := st.GetScanNames(ctx)
	r.NoError(err)
	assert.Empty(t, names)

	r.NoError(st.PutScan(ctx, testScan("test1")))
	names, err = st.GetScanNames(ctx)
	r.NoError(err)
	assert.Equal(t, []string{"test1"}, names)

	r.NoError(st.PutScan(ctx, testScan("test2")))
	names, err = st.GetScanNames(ctx)
	r.NoError(err)
	assert.ElementsMatch(t, []string{"test1", "test2"}, names)
}

// TestDeleteScan verifies that deleting a scan removes it, and its
// patterns, from the store.
func TestDeleteScan(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	r := require.New(t)
	st, _ := testutil.NewStore(ctx, t)

	scan := testScan("test")
	scan.Patterns = []store.Pattern{{Name: "cdc", Regex: "cdc", Help: "cdc events"}}
	r.NoError(st.PutScan(ctx, scan))

	r.NoError(st.DeleteScan(ctx, "test"))

	got, err := st.GetScan(ctx, "test")
	r.NoError(err)
	assert.Nil(t, got)
	patterns, err := st.GetScanPatterns(ctx, "test")
	r.NoError(err)
	assert.Empty(t, patterns)
}

// TestGetScan verifies we can get a scan definition from the database.
func TestGetScan(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	r := require.New(t)
	st, _ := testutil.NewStore(ctx, t)

	got, err := st.GetScan(ctx, "none")
	r.NoError(err)
	assert.Nil(t, got)

	noPatterns := testScan("no_patterns")
	withPatterns := testScan("with_patterns")
	withPatterns.Patterns = []store.Pattern{
		{Name: "cdc", Regex: "cdc", Help: "cdc events"},
		{Name: "kv", Regex: "kv", Exclude: "exclude", Help: "kv events"},
	}

	for _, want := range []*store.Scan{noPatterns, withPatterns} {
		r.NoError(st.PutScan(ctx, want))

		got, err := st.GetScan(ctx, want.Name)
		r.NoError(err)
		r.NotNil(got)
		assert.WithinDuration(t, time.Now(), got.LastModified.Time, time.Minute)
		got.LastModified = want.LastModified
		assert.Equal(t, want, got)
	}
}

// TestPutScan verifies that a scan can be uploaded, and that putting it
// again with different patterns replaces the previous ones.
func TestPutScan(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	r := require.New(t)
	st, _ := testutil.NewStore(ctx, t)

	scan := testScan("with_patterns")
	scan.Patterns = []store.Pattern{
		{Name: "cdc", Regex: "cdc", Help: "cdc events"},
		{Name: "kv", Regex: "kv", Exclude: "exclude", Help: "kv events"},
	}
	r.NoError(st.PutScan(ctx, scan))

	got, err := st.GetScan(ctx, scan.Name)
	r.NoError(err)
	r.NotNil(got)
	got.LastModified = scan.LastModified
	assert.Equal(t, scan, got)

	// Putting the scan again with fewer patterns must replace, not append
	// to, the previous set.
	scan.Patterns = []store.Pattern{{Name: "cdc", Regex: "cdc", Help: "cdc events"}}
	r.NoError(st.PutScan(ctx, scan))

	got, err = st.GetScan(ctx, scan.Name)
	r.NoError(err)
	r.NotNil(got)
	got.LastModified = scan.LastModified
	assert.Equal(t, scan, got)
}
