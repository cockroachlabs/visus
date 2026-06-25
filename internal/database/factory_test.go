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

package database

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestNewPingsUnreachableDatabase verifies that New fails when the database is
// not reachable. pgxpool.NewWithConfig is lazy and does not open a connection,
// so without the ping check New would return a "successful" pool pointing at an
// unreachable host. The test reserves a TCP port and immediately releases it so
// the URL is syntactically valid but no server is listening.
func TestNewPingsUnreachableDatabase(t *testing.T) {
	r := require.New(t)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	r.NoError(err)
	addr := listener.Addr().(*net.TCPAddr)
	r.NoError(listener.Close())

	url := "postgres://test@" + addr.String() + "/test?sslmode=disable&connect_timeout=1"

	// Context expires before the retry backoff has a chance to grow, so the
	// test stays fast even though the retry loop is exercised.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	pool, err := New(ctx, url)
	r.Error(err)
	r.Nil(pool)
}
