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

package scanner

import (
	"context"
	"testing"
	"time"

	"github.com/cockroachlabs/visus/internal/server"
	"github.com/cockroachlabs/visus/internal/stopper"
	"github.com/cockroachlabs/visus/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRefreshScanners(t *testing.T) {
	r := require.New(t)
	a := assert.New(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	stopperCtx := stopper.WithContext(ctx)
	mockStore := &store.Memory{}
	mockStore.Init(ctx)
	cfg := &server.Config{}
	registry := prometheus.NewRegistry()
	scanners := &scannerServer{
		config:        cfg,
		fromBeginning: true,
		registry:      registry,
		store:         mockStore,
	}
	scanners.mu.scanners = make(map[string]*Scanner)

	now := time.Now()
	crdbScan := &store.Scan{
		Enabled: true,
		Format:  store.CRDBV2,
		LastModified: pgtype.Timestamp{
			Time:  now,
			Valid: true,
		},
		Name: "crdblog",
		Path: "./testdata/sample.log",
		Patterns: []store.Pattern{
			{
				Name:  "cache",
				Regex: "cache size",
				Help:  "cache events",
			},
			{
				Name:  "max",
				Regex: "max size",
				Help:  "max events",
			},
		},
	}
	// Adding the scan to the store
	mockStore.PutScan(ctx, crdbScan)
	// Start the scanner
	err := scanners.Start(stopperCtx)
	r.NoError(err)
	// Making sure that the scan was added
	crdbScanner, ok := scanners.get(crdbScan.Name)
	r.True(ok)
	a.Equal(now, crdbScanner.GetLastModified())
	r.NoError(waitUntilRunning(ctx, crdbScanner))
	// Waiting for the 2 metrics to show up.
	r.NoError(waitForMetrics(ctx, registry, 2))
	// Refresh again
	err = scanners.Refresh(stopperCtx)
	r.NoError(err)
	// Making sure that the scan is still there
	crdbScanner, ok = scanners.get(crdbScan.Name)
	r.True(ok)
	a.Equal(now, crdbScanner.GetLastModified())
	// Updating the scan
	now = time.Now()
	crdbScan.LastModified.Time = now
	mockStore.PutScan(ctx, crdbScan)
	// Refresh
	err = scanners.Refresh(stopperCtx)
	r.NoError(err)
	// Making sure that the scan was updated
	crdbScanner, ok = scanners.get(crdbScan.Name)
	r.True(ok)
	a.Equal(now, crdbScanner.GetLastModified())

	// Adding another scan
	pebbleTime := time.Now()
	pebbleScan := &store.Scan{
		Enabled: true,
		Format:  store.CRDBV2,
		LastModified: pgtype.Timestamp{
			Time:  pebbleTime,
			Valid: true,
		},
		Name: "pebble",
		Path: "./testdata/pebble.log",
		Patterns: []store.Pattern{
			{
				Name:  "sstable",
				Regex: "sstable",
				Help:  "sstable events",
			},
		},
	}

	// Adding the scan to the store
	mockStore.PutScan(ctx, pebbleScan)

	// Refresh
	err = scanners.Refresh(stopperCtx)
	r.NoError(err)

	// Making sure that the crdb scan is still there
	crdbScanner, ok = scanners.get(crdbScan.Name)
	r.True(ok)
	a.Equal(now, crdbScanner.GetLastModified())
	r.NoError(waitUntilRunning(ctx, crdbScanner))
	// Making sure that the pebble scan  there
	pebbleScanner, ok := scanners.get(pebbleScan.Name)
	r.True(ok)
	a.Equal(pebbleTime, pebbleScanner.GetLastModified())
	r.NoError(waitUntilRunning(ctx, pebbleScanner))
	// Waiting for the additional metric to show up.
	r.NoError(waitForMetrics(ctx, registry, 3))

	// Removing the crdb log scanner
	mockStore.DeleteScan(ctx, crdbScan.Name)
	// Refresh
	err = scanners.Refresh(stopperCtx)
	r.NoError(err)

	// Making sure that the crdb scanner is stopped, and pebble is not
	r.True(crdbScanner.stopped())
	r.False(pebbleScanner.stopped())

	// Making sure that the crdb scanner is gone
	crdbScanner, ok = scanners.get(crdbScan.Name)
	a.False(ok)
	a.Nil(crdbScanner)

	// Making sure that the pebble scan  there
	pebbleScanner, ok = scanners.get(pebbleScan.Name)
	r.True(ok)
	a.Equal(pebbleTime, pebbleScanner.GetLastModified())

	// Verify that we collected metrics
	metrics, err := registry.Gather()
	r.NoError(err)
	a.Equal(3, len(metrics))
	for _, m := range metrics {
		a.Contains([]string{"crdblog_cache", "crdblog_max", "pebble_sstable"}, *m.Name)
	}

}

func waitForMetrics(ctx context.Context, registry *prometheus.Registry, expected int) error {
	for {
		metrics, err := registry.Gather()
		if err != nil {
			return err
		}
		if len(metrics) == expected {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.Tick(time.Second):
		}
	}

}
func waitUntilRunning(ctx context.Context, scanner *Scanner) error {
	for scanner.stopped() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.Tick(time.Second):
		}
	}
	return nil
}
