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

package database_test

import (
	"context"
	"testing"
	"time"

	"github.com/cockroachlabs/visus/internal/database"
	"github.com/cockroachlabs/visus/internal/testutil"
	"github.com/stretchr/testify/require"
)

// allowUnsafeInternalsSetting returns the value of the allow_unsafe_internals
// session variable on the given connection, so tests can confirm the
// --allow-unsafe-internals flag actually reaches the connection instead of
// merely being accepted on the command line.
func allowUnsafeInternalsSetting(
	ctx context.Context, t *testing.T, conn database.Connection,
) string {
	t.Helper()
	var value string
	require.NoError(t, conn.QueryRow(ctx, "SHOW allow_unsafe_internals").Scan(&value))
	return value
}

// skipIfAllowUnsafeInternalsUnsupported skips the calling test on
// CockroachDB versions that predate the allow_unsafe_internals session
// variable (e.g. 24.3), since ReadOnly would otherwise fail there: its
// connection pool retries the failing AfterConnect hook until the context
// deadline instead of surfacing the "unrecognized configuration parameter"
// error directly. A plain connection without the flag detects support
// without paying that retry cost.
func skipIfAllowUnsafeInternalsUnsupported(ctx context.Context, t *testing.T) {
	t.Helper()
	conn, err := database.New(ctx, testutil.PGURL().String())
	require.NoError(t, err)
	if _, err := conn.Exec(ctx, "SHOW allow_unsafe_internals"); err != nil {
		t.Skipf("allow_unsafe_internals not supported by this CockroachDB version: %v", err)
	}
}

// TestReadOnlyAllowUnsafeInternals verifies that ReadOnly's
// allowUnsafeInternals argument actually sets allow_unsafe_internals on the
// connection's session, rather than only being threaded through as an
// unused parameter.
func TestReadOnlyAllowUnsafeInternals(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	r := require.New(t)
	skipIfAllowUnsafeInternalsUnsupported(ctx, t)

	conn, err := database.ReadOnly(ctx, testutil.PGURL().String(), true /* allowUnsafeInternals */)
	r.NoError(err)

	r.Equal("on", allowUnsafeInternalsSetting(ctx, t, conn))
}

// TestReadOnlyDefaultDisallowsUnsafeInternals verifies that ReadOnly doesn't
// force allow_unsafe_internals on when the flag isn't requested. It compares
// against a plain connection's baseline rather than a hardcoded "off",
// since the cluster's own default for the setting varies by CockroachDB
// version (e.g. root defaults to "on" on 25.4, but not on later versions).
func TestReadOnlyDefaultDisallowsUnsafeInternals(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	r := require.New(t)
	skipIfAllowUnsafeInternalsUnsupported(ctx, t)

	baseline, err := database.New(ctx, testutil.PGURL().String())
	r.NoError(err)
	baselineValue := allowUnsafeInternalsSetting(ctx, t, baseline)

	conn, err := database.ReadOnly(ctx, testutil.PGURL().String(), false /* allowUnsafeInternals */)
	r.NoError(err)

	r.Equal(baselineValue, allowUnsafeInternalsSetting(ctx, t, conn),
		"ReadOnly with allowUnsafeInternals=false should not change the connection's baseline setting")
}

// TestReadOnlySetsFollowerReads verifies that ReadOnly always enables
// default_transaction_use_follower_reads on the connection's session,
// independent of allowUnsafeInternals.
func TestReadOnlySetsFollowerReads(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	r := require.New(t)

	conn, err := database.ReadOnly(ctx, testutil.PGURL().String(), false /* allowUnsafeInternals */)
	r.NoError(err)

	var value string
	r.NoError(conn.QueryRow(ctx, "SHOW default_transaction_use_follower_reads").Scan(&value))
	r.Equal("on", value)
}

// TestNewDoesNotSetFollowerReads verifies that a plain, non-read-only
// connection leaves default_transaction_use_follower_reads at its default.
func TestNewDoesNotSetFollowerReads(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	r := require.New(t)

	conn, err := database.New(ctx, testutil.PGURL().String())
	r.NoError(err)

	var value string
	r.NoError(conn.QueryRow(ctx, "SHOW default_transaction_use_follower_reads").Scan(&value))
	r.Equal("off", value)
}
