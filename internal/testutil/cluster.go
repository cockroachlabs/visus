// Copyright 2026 The Cockroach Authors
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
//
// SPDX-License-Identifier: Apache-2.0

//go:build integration

// Package testutil provides shared fixtures for tests that run against a
// real CockroachDB cluster. It is only compiled under the "integration"
// build tag so that ordinary unit tests, and any production build, never
// link the testserver dependency.
package testutil

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/cockroachdb/cockroach-go/v2/testserver"
	"github.com/cockroachlabs/visus/internal/database"
	"github.com/cockroachlabs/visus/internal/store"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// crdbTestSeriesEnv optionally pins the shared test cluster to the latest
// patch release of a CockroachDB release series, e.g. "25.4". This is how
// CI runs the same suite against multiple CRDB versions. When unset,
// testserver downloads the latest stable release.
const crdbTestSeriesEnv = "CRDB_TEST_SERIES"

// crdbReleaseDataURL is the release feed cockroach-go/v2/testserver itself
// uses to resolve "latest stable"; reusing it here lets StartCluster
// resolve "latest patch in a given series" the same way.
const crdbReleaseDataURL = "https://raw.githubusercontent.com/cockroachdb/docs/main/src/current/_data/releases.yml"

// pgURL holds the connection URL for the shared cluster started by
// StartCluster. It is read by PGURL once the cluster is up.
var pgURL *url.URL

// StartCluster starts a single CockroachDB testserver shared by every test
// in the calling package, runs the test suite, and returns the process
// exit code. Every package with integration tests needs exactly one
// TestMain calling this, since Go only allows one TestMain per package:
//
//	func TestMain(m *testing.M) {
//	    os.Exit(testutil.StartCluster(m))
//	}
//
// The server is stopped before StartCluster returns.
func StartCluster(m *testing.M) int {
	var opts []testserver.TestServerOpt
	if series := os.Getenv(crdbTestSeriesEnv); series != "" {
		version, err := latestPatchInSeries(series)
		if err != nil {
			log.Printf("testutil: resolving latest %s patch: %v", series, err)
			return 1
		}
		log.Printf("testutil: pinning cluster to %s (latest %s patch)", version, series)
		opts = append(opts, testserver.CustomVersionOpt(version))
	}

	ts, err := testserver.NewTestServer(opts...)
	if err != nil {
		return 1
	}
	defer ts.Stop()

	pgURL = ts.PGURL()
	return m.Run()
}

// crdbRelease is the subset of fields we need from crdbReleaseDataURL's
// YAML feed.
type crdbRelease struct {
	Name      string `yaml:"release_name"`
	Withdrawn bool   `yaml:"withdrawn"`
	CloudOnly bool   `yaml:"cloud_only"`
}

// latestPatchInSeries returns the newest non-withdrawn, downloadable
// release in the given major.minor series (e.g. "25.4" -> "v25.4.16"), so
// callers don't need to hardcode a patch version that goes stale.
func latestPatchInSeries(series string) (string, error) {
	resp, err := http.Get(crdbReleaseDataURL)
	if err != nil {
		return "", fmt.Errorf("downloading release data: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading release data: %w", err)
	}

	var releases []crdbRelease
	if err := yaml.Unmarshal(body, &releases); err != nil {
		return "", fmt.Errorf("parsing release data: %w", err)
	}

	pattern := regexp.MustCompile(`^v` + regexp.QuoteMeta(series) + `\.(\d+)$`)
	best, bestPatch := "", -1
	for _, r := range releases {
		if r.Withdrawn || r.CloudOnly {
			continue
		}
		m := pattern.FindStringSubmatch(r.Name)
		if m == nil {
			continue
		}
		if patch, err := strconv.Atoi(m[1]); err == nil && patch > bestPatch {
			bestPatch, best = patch, r.Name
		}
	}
	if best == "" {
		return "", fmt.Errorf("no stable, non-withdrawn release found for series %q", series)
	}
	return best, nil
}

// PGURL returns the connection URL for the shared cluster started by
// StartCluster. It panics if called before StartCluster has set the URL,
// which only happens if a package forgets to wire up TestMain.
func PGURL() *url.URL {
	if pgURL == nil {
		panic("testutil.PGURL called before StartCluster; missing TestMain in this package?")
	}
	u := *pgURL
	return &u
}

// NewStore drops and recreates the _visus database on the shared cluster,
// giving the test an empty schema, then returns a Store and the underlying
// connection backing it.
//
// The _visus store schema is a fixed database name (see
// internal/store/sql/ddl.sql), not something a test can point at a
// per-test logical database, so isolation between tests in the same
// package comes from resetting the schema rather than from separate
// databases. Tests that call NewStore must not run in parallel with each
// other.
func NewStore(ctx context.Context, t *testing.T) (store.Store, database.Connection) {
	t.Helper()
	r := require.New(t)

	conn, err := database.New(ctx, PGURL().String())
	r.NoError(err)

	_, err = conn.Exec(ctx, "DROP DATABASE IF EXISTS _visus CASCADE")
	r.NoError(err)

	st := store.New(conn)
	r.NoError(st.Init(ctx))

	return st, conn
}

// invalidDBChars matches everything that isn't safe to use unquoted in a
// CockroachDB database identifier.
var invalidDBChars = regexp.MustCompile(`[^a-z0-9_]+`)

// NewDatabase creates a scratch database, named after the calling test, on
// the shared cluster and returns a connection to it along with the
// database's name. This is for tests that need their own tables outside of
// the fixed _visus schema (see NewStore). The database is dropped when the
// test completes.
func NewDatabase(ctx context.Context, t *testing.T) (database.Connection, string) {
	t.Helper()
	r := require.New(t)

	name := invalidDBChars.ReplaceAllString(strings.ToLower(t.Name()), "_")

	admin, err := database.New(ctx, PGURL().String())
	r.NoError(err)
	_, err = admin.Exec(ctx, "CREATE DATABASE IF NOT EXISTS "+name)
	r.NoError(err)
	t.Cleanup(func() {
		_, err := admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+name+" CASCADE")
		if err != nil {
			t.Logf("testutil: dropping database %s: %v", name, err)
		}
	})

	u := PGURL()
	u.Path = "/" + name
	conn, err := database.New(ctx, u.String())
	r.NoError(err)
	return conn, name
}
