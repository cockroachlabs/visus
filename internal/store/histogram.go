// Copyright 2022 Cockroach Labs Inc.
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

	"github.com/jackc/pgtype"
	log "github.com/sirupsen/logrus"
)

// Histogram stores the properties for a histogram definition.
// Matching histograms will be converted into linear log10 histograms.
type Histogram struct {
	Enabled      bool             // Enabled is true if the histograms needs to be translated.
	Name         string           // Name of the config rule
	Bins         int              // Bins is the number of linear bins with a logarithm bucket.
	Start        int              // Start is the minimun value in the histogram
	End          int              // End is the maximum value in the histogram
	Regex        string           // Regex to match for incoming histograms to be converted.
	LastModified pgtype.Timestamp // LastModified when the definition was updated.
}

//go:embed sql/getHistogram.sql
var getHistogramStmt string

//go:embed sql/listHistograms.sql

var listHistogramsStmt string

//go:embed sql/upsertHistogram.sql
var upsertHistogramStmt string

//go:embed sql/deleteHistogram.sql
var deleteHistogramStmt string

// DeleteHistogram removes a histogram configuration from the database.
func (s *store) DeleteHistogram(ctx context.Context, regex string) error {
	txn, err := s.conn.Begin(ctx)
	if err != nil {
		log.Debugln(err)
		return err
	}
	defer txn.Commit(ctx)
	_, err = txn.Exec(ctx, deleteHistogramStmt, regex)
	if err != nil {
		log.Errorf("delete histogram error:%s", err)
		txn.Rollback(ctx)
		return err
	}
	return nil
}

// GetHistograms retrieves all the histograms stored in the database.
func (s *store) GetHistogram(ctx context.Context, name string) (*Histogram, error) {
	rows, err := s.conn.Query(ctx, getHistogramStmt, name)
	if err != nil {
		log.Errorf("GetHistogram %s ", err.Error())
		return nil, err
	}
	defer rows.Close()
	if rows.Next() {
		histogram := &Histogram{}
		err := rows.Scan(
			&histogram.Name, &histogram.Regex,
			&histogram.LastModified, &histogram.Enabled,
			&histogram.Bins, &histogram.Start, &histogram.End)
		if err != nil {
			log.Debugln(err)
			return nil, err
		}
		return histogram, nil
	}
	return nil, err
}

// GetHistograms retrieves all the histograms stored in the database.
func (s *store) GetHistogramNames(ctx context.Context) ([]string, error) {
	rows, err := s.conn.Query(ctx, listHistogramsStmt)
	if err != nil {
		log.Errorf("GetHistogramNames %s ", err.Error())
		return nil, err
	}
	defer rows.Close()
	res := make([]string, 0)

	for rows.Next() {
		var name string
		err := rows.Scan(&name)
		if err != nil {
			log.Debugln(err)
			return nil, err
		}
		res = append(res, name)
	}
	return res, nil
}

// PutHistogram adds a new histogram configuration to the database.
// If a histogram with the same regex already exists, it is replaced.
func (s *store) PutHistogram(ctx context.Context, histogram *Histogram) error {
	log.Debugf("%+v", histogram)
	txn, err := s.conn.Begin(ctx)
	if err != nil {
		log.Debugln(err)
		return err
	}
	defer txn.Commit(ctx)
	_, err = txn.Exec(ctx, upsertHistogramStmt,
		histogram.Name,
		histogram.Regex, histogram.Bins,
		histogram.Start, histogram.End)
	if err != nil {
		log.Errorf("upsert histogram error:%s, %s", upsertHistogramStmt, err)
		txn.Rollback(ctx)
		return err
	}
	return nil
}
