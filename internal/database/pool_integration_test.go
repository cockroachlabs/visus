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

// TestPoolRefresh verifies that Refresh installs a new underlying pool and
// closes the old one, rather than merely reconnecting in place. This is the
// path exercised on SIGHUP to pick up rotated certificates.
func TestPoolRefresh(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	r := require.New(t)

	pool, err := database.New(ctx, testutil.PGURL().String())
	r.NoError(err)

	oldPgxPool := database.PoolUnderlying(pool)
	r.NoError(oldPgxPool.Ping(ctx))

	r.NoError(pool.Refresh(ctx))

	newPgxPool := database.PoolUnderlying(pool)
	r.NotSame(oldPgxPool, newPgxPool, "Refresh should install a new underlying pool")
	r.Error(oldPgxPool.Ping(ctx), "Refresh should close the old pool")

	var one int
	r.NoError(pool.QueryRow(ctx, "SELECT 1").Scan(&one))
	r.Equal(1, one)
}
