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

package store

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/pashagolub/pgxmock/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetNodes(t *testing.T) {
	mock, err := pgxmock.NewConn()
	require.NoError(t, err)
	defer mock.Close(context.Background())
	store := New(mock)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	now := time.Now()
	columns := []string{"id", "hostname", "pid", "updated"}
	rows := mock.NewRows(columns).
		AddRow(int64(100), "host-a", 1234, now).
		AddRow(int64(200), "host-b", 5678, now.Add(-1*time.Minute))
	mock.ExpectQuery("SELECT id, hostname, pid, updated").WillReturnRows(rows)

	nodes, err := store.GetNodes(ctx)
	require.NoError(t, err)
	require.Len(t, nodes, 2)
	assert.Equal(t, int64(100), nodes[0].ID)
	assert.Equal(t, "host-a", nodes[0].Hostname)
	assert.Equal(t, 1234, nodes[0].PID)
	assert.Equal(t, int64(200), nodes[1].ID)
	assert.Equal(t, "host-b", nodes[1].Hostname)
	assert.Equal(t, 5678, nodes[1].PID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestGetNodesEmpty(t *testing.T) {
	mock, err := pgxmock.NewConn()
	require.NoError(t, err)
	defer mock.Close(context.Background())
	store := New(mock)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	columns := []string{"id", "hostname", "pid", "updated"}
	rows := mock.NewRows(columns)
	mock.ExpectQuery("SELECT id, hostname, pid, updated").WillReturnRows(rows)

	nodes, err := store.GetNodes(ctx)
	require.NoError(t, err)
	assert.Empty(t, nodes)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRegisterNode(t *testing.T) {
	mock, err := pgxmock.NewConn()
	require.NoError(t, err)
	defer mock.Close(context.Background())
	store := New(mock)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	mock.ExpectQuery("INSERT INTO _visus.node").
		WithArgs("myhost", 42).
		WillReturnRows(mock.NewRows([]string{"id"}).AddRow(int64(999)))

	id, err := store.RegisterNode(ctx, "myhost", 42)
	require.NoError(t, err)
	assert.Equal(t, int64(999), id)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestHeartbeat(t *testing.T) {
	mock, err := pgxmock.NewConn()
	require.NoError(t, err)
	defer mock.Close(context.Background())
	store := New(mock)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	mock.ExpectExec("UPDATE _visus.node").
		WithArgs(int64(999)).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err = store.Heartbeat(ctx, 999)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDeleteNode(t *testing.T) {
	mock, err := pgxmock.NewConn()
	require.NoError(t, err)
	defer mock.Close(context.Background())
	store := New(mock)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	mock.ExpectExec("DELETE FROM _visus.node WHERE").
		WithArgs(int64(999)).
		WillReturnResult(pgxmock.NewResult("DELETE", 1))

	err = store.DeleteNode(ctx, 999)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDeleteNodeError(t *testing.T) {
	mock, err := pgxmock.NewConn()
	require.NoError(t, err)
	defer mock.Close(context.Background())
	store := New(mock)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	mock.ExpectExec("DELETE FROM _visus.node WHERE").
		WithArgs(int64(999)).
		WillReturnError(fmt.Errorf("connection lost"))

	err = store.DeleteNode(ctx, 999)
	assert.ErrorContains(t, err, "connection lost")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestGetNodesQueryError(t *testing.T) {
	mock, err := pgxmock.NewConn()
	require.NoError(t, err)
	defer mock.Close(context.Background())
	store := New(mock)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	mock.ExpectQuery("SELECT id, hostname, pid, updated").
		WillReturnError(fmt.Errorf("connection refused"))

	_, err = store.GetNodes(ctx)
	assert.ErrorContains(t, err, "connection refused")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRegisterNodeError(t *testing.T) {
	mock, err := pgxmock.NewConn()
	require.NoError(t, err)
	defer mock.Close(context.Background())
	store := New(mock)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	mock.ExpectQuery("INSERT INTO _visus.node").
		WithArgs("myhost", 42).
		WillReturnError(fmt.Errorf("duplicate key"))

	_, err = store.RegisterNode(ctx, "myhost", 42)
	assert.ErrorContains(t, err, "duplicate key")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestHeartbeatError(t *testing.T) {
	mock, err := pgxmock.NewConn()
	require.NoError(t, err)
	defer mock.Close(context.Background())
	store := New(mock)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	mock.ExpectExec("UPDATE _visus.node").
		WithArgs(int64(999)).
		WillReturnError(fmt.Errorf("timeout"))

	err = store.Heartbeat(ctx, 999)
	assert.ErrorContains(t, err, "timeout")
	require.NoError(t, mock.ExpectationsWereMet())
}
