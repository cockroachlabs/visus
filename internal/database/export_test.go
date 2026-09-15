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

// This file exposes package-private state to tests in database_test. It
// cannot import internal/testutil itself: testutil imports database, and
// this file's package declaration (database, not database_test) would
// create an import cycle if it did.
package database

import "github.com/jackc/pgx/v5/pgxpool"

// PoolUnderlying returns the pgxpool.Pool a Pool currently delegates to, so
// tests can confirm Refresh actually swaps and closes it rather than just
// observing that queries keep working afterward.
func PoolUnderlying(p *Pool) *pgxpool.Pool {
	return p.get()
}
