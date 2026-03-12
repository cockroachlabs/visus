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
	_ "embed" // embedding sql statements
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pkg/errors"
)

// NodeInfo represents a registered node.
// table schema: id, hostname, pid, version, updated
type NodeInfo struct {
	ID       int64
	Hostname string
	PID      int
	Version  string
	Updated  time.Time
}

//go:embed sql/listNodes.sql
var listNodesStmt string

//go:embed sql/registerNode.sql
var registerNodeStmt string

//go:embed sql/heartbeatNode.sql
var heartbeatNodeStmt string

//go:embed sql/deleteNode.sql
var deleteNodeStmt string

// GetNodes returns information about all registered nodes.
func (s *store) GetNodes(ctx context.Context) ([]NodeInfo, error) {
	rows, err := s.conn.Query(ctx, listNodesStmt)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	defer rows.Close()

	return pgx.CollectRows(rows, pgx.RowToStructByNameLax[NodeInfo])
}

// RegisterNode inserts a new node and returns the auto-generated ID.
func (s *store) RegisterNode(
	ctx context.Context, hostname string, pid int, version string,
) (int64, error) {
	row := s.conn.QueryRow(ctx, registerNodeStmt, hostname, pid, version)
	var id int64
	if err := row.Scan(&id); err != nil {
		return 0, errors.WithStack(err)
	}
	return id, nil
}

// Heartbeat updates the heartbeat timestamp for a node.
func (s *store) Heartbeat(ctx context.Context, id int64) error {
	_, err := s.conn.Exec(ctx, heartbeatNodeStmt, id)
	return errors.WithStack(err)
}

// DeleteNode removes a single node by ID.
func (s *store) DeleteNode(ctx context.Context, id int64) error {
	_, err := s.conn.Exec(ctx, deleteNodeStmt, id)
	return errors.WithStack(err)
}
