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

package store

import (
	"context"
	_ "embed" // embedding sql statements
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

// LogFormat defines of the log.
type LogFormat string

const (
	// CRDBv2 is the format of the cockroach log.
	CRDBv2 = LogFormat("crdb-v2")
	// CRDBv2Auth is the format of the cockroach auth log.
	CRDBv2Auth = LogFormat("crdb-v2-auth")
)

// A Pattern defines the regular expression to match to increase the pattern counter
type Pattern struct {
	Help  string // Help to used to describe the pattern.
	Name  string // Name of the pattern, which represents the property being measured.
	Regex string // Kind is the type of the pattern.
}

// Scan defines the list of patterns that used to scan a log file.
// This struct defines the configuration for the log.
type Scan struct {
	Enabled      bool             // Enabled is true if the patterns needs to be collected.
	Format       LogFormat        // Format of the log.
	LastModified pgtype.Timestamp // LastModified the last time the log was updated in the database.
	Name         string           // Name of the log
	Path         string           // Location of the log file
	Patterns     []Pattern        // The patterns to look for
}

//go:embed sql/listScans.sql
var listScansStmt string

//go:embed sql/getScan.sql
var getScanStmt string

//go:embed sql/getPatterns.sql
var getPatternsStmt string

//go:embed sql/upsertScan.sql
var upsertScanStmt string

//go:embed sql/insertPattern.sql
var insertPatternStmt string

//go:embed sql/deleteScan.sql
var deleteScanStmt string

//go:embed sql/deletePatterns.sql
var deletePatternsStmt string

// DeleteScan removes a scan configuration and associated patterns from the database.
func (s *store) DeleteScan(ctx context.Context, name string) error {
	txn, err := s.conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if err := txn.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			log.Errorf("rollback failed %s", err)
		}
	}()
	_, err = txn.Exec(ctx, deletePatternsStmt, name)
	if err != nil {
		return errors.WithStack(err)
	}
	_, err = txn.Exec(ctx, deleteScanStmt, name)
	if err != nil {
		return errors.WithStack(err)
	}
	return txn.Commit(ctx)
}

// GetScanNames retrieves all the log names stored in the database.
func (s *store) GetScanNames(ctx context.Context) ([]string, error) {
	rows, err := s.conn.Query(ctx, listScansStmt)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	res := make([]string, 0)
	for rows.Next() {
		var name string
		err := rows.Scan(&name)
		if err != nil {
			return nil, errors.WithStack(err)
		}
		res = append(res, name)
	}
	return res, nil
}

// GetScan retrieves the log configuration from the database
func (s *store) GetScan(ctx context.Context, name string) (*Scan, error) {
	scan := &Scan{}
	row := s.conn.QueryRow(ctx, getScanStmt, name)
	if err := row.Scan(&scan.Name, &scan.Path, &scan.Format,
		&scan.LastModified, &scan.Enabled); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.WithStack(err)
	}
	patterns, err := s.GetScanPatterns(ctx, name)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	scan.Patterns = patterns
	return scan, nil
}

// GetScanPatterns retrieves the configuration for the patterns associated to this log.
func (s *store) GetScanPatterns(ctx context.Context, name string) ([]Pattern, error) {
	patterns := make([]Pattern, 0)
	rows, err := s.conn.Query(ctx, getPatternsStmt, name)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	defer rows.Close()
	for rows.Next() {
		pattern := Pattern{}
		err := rows.Scan(&pattern.Name, &pattern.Regex, &pattern.Help)
		if err != nil {
			return nil, errors.WithStack(err)
		}
		patterns = append(patterns, pattern)
	}
	return patterns, nil
}

// PutScan adds a new log configuration to the database.
// If a log with the same name already exists, it is replaced.
func (s *store) PutScan(ctx context.Context, target *Scan) error {
	log.Tracef("PutScan %+v", target)
	txn, err := s.conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if err := txn.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			log.Errorf("rollback failed %s", err)
		}
	}()
	_, err = txn.Exec(ctx, deletePatternsStmt, target.Name)
	if err != nil {
		return errors.WithStack(err)
	}
	_, err = txn.Exec(ctx, upsertScanStmt,
		target.Name, target.Path, target.Format, target.Enabled)
	if err != nil {
		return errors.WithStack(err)
	}
	for _, pattern := range target.Patterns {
		_, err = txn.Exec(ctx, insertPatternStmt, target.Name, pattern.Name,
			pattern.Regex, pattern.Help)
		if err != nil {
			return errors.WithStack(err)
		}
	}
	return txn.Commit(ctx)
}

// String implements fmt.Stringer
func (c *Scan) String() string {
	return fmt.Sprintf(`Scan:     %s
Enabled:  %t
Updated:  %s
Format:   %s
Path:     %s
Patterns: %v`,
		c.Name, c.Enabled, c.LastModified.Time, c.Path, c.Format, c.Patterns)
}
