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
	"slices"
	"sync"
	"time"
)

// Memory stores the configuration in memory. Used for testing.
type Memory struct {
	collections *sync.Map
	histograms  *sync.Map
	scans       *sync.Map
	nodes       *sync.Map
	now         func() time.Time
	mu          struct {
		sync.RWMutex
		err        error // Error to return
		nextNodeID int64
	}
}

var _ Store = &Memory{}

// NewMemoryStore creates an in-memory store with a user-supplied
// function to determine the current time.
func NewMemoryStore(now func() time.Time) *Memory {
	return &Memory{
		now: now,
	}
}

// DeleteCollection implements store.Store.
func (m *Memory) DeleteCollection(_ context.Context, name string) error {
	if m.Error() != nil {
		return m.Error()
	}
	m.collections.Delete(name)
	return nil
}

// DeleteHistogram implements store.Store.
func (m *Memory) DeleteHistogram(_ context.Context, name string) error {
	if m.Error() != nil {
		return m.Error()
	}
	m.histograms.Delete(name)
	return nil
}

// DeleteScan implements store.Store.
func (m *Memory) DeleteScan(_ context.Context, name string) error {
	if m.Error() != nil {
		return m.Error()
	}
	m.scans.Delete(name)
	return nil
}

func (m *Memory) nextID() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mu.nextNodeID++
	return m.mu.nextNodeID
}

// Error returns the injected error
func (m *Memory) Error() error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.mu.err
}

// GetCollection implements store.Store.
func (m *Memory) GetCollection(_ context.Context, name string) (*Collection, error) {
	if m.Error() != nil {
		return nil, m.Error()
	}
	res, ok := m.collections.Load(name)
	if !ok {
		return nil, nil
	}
	return res.(*Collection), nil
}

// GetCollectionNames implements store.Store.
func (m *Memory) GetCollectionNames(_ context.Context) ([]string, error) {
	return m.getNames(m.collections)
}

// GetHistogram implements store.Store.
func (m *Memory) GetHistogram(_ context.Context, name string) (*Histogram, error) {
	if m.Error() != nil {
		return nil, m.Error()
	}
	res, ok := m.histograms.Load(name)
	if !ok {
		return nil, m.Error()
	}
	return res.(*Histogram), m.Error()
}

// GetHistogramNames implements store.Store.
func (m *Memory) GetHistogramNames(_ context.Context) ([]string, error) {
	return m.getNames(m.histograms)
}

// GetMetrics implements store.Store.
func (m *Memory) GetMetrics(ctx context.Context, name string) ([]Metric, error) {
	if m.Error() != nil {
		return nil, m.Error()
	}
	coll, _ := m.GetCollection(ctx, name)
	return coll.Metrics, nil
}

// GetNodes implements store.Store.
func (m *Memory) GetNodes(_ context.Context) ([]NodeInfo, error) {
	if m.Error() != nil {
		return nil, m.Error()
	}
	var nodes []NodeInfo
	m.nodes.Range(func(key any, value any) bool {
		nodes = append(nodes, value.(NodeInfo))
		return true
	})
	slices.SortFunc(nodes, func(a, b NodeInfo) int {
		return b.Updated.Compare(a.Updated)
	})
	return nodes, nil
}

// RegisterNode implements store.Store.
func (m *Memory) RegisterNode(
	_ context.Context, hostname string, pid int, version string,
) (int64, error) {
	if m.Error() != nil {
		return 0, m.Error()
	}
	id := m.nextID()
	m.nodes.Store(id, NodeInfo{
		ID:       id,
		Hostname: hostname,
		PID:      pid,
		Version:  version,
		Updated:  m.now(),
	})
	return id, nil
}

// Heartbeat implements store.Store.
func (m *Memory) Heartbeat(_ context.Context, id int64) error {
	if m.Error() != nil {
		return m.Error()
	}
	val, ok := m.nodes.Load(id)
	if !ok {
		return nil
	}
	n := val.(NodeInfo)
	n.Updated = m.now()
	m.nodes.Store(id, n)
	return nil
}

// DeleteNode implements store.Store.
func (m *Memory) DeleteNode(_ context.Context, id int64) error {
	if m.Error() != nil {
		return m.Error()
	}
	m.nodes.Delete(id)
	return nil
}

// GetScan implements store.Store.
func (m *Memory) GetScan(_ context.Context, name string) (*Scan, error) {
	if m.Error() != nil {
		return nil, m.Error()
	}
	res, ok := m.scans.Load(name)
	if !ok {
		return nil, nil
	}
	return res.(*Scan), nil
}

// GetScanNames implements store.Store.
func (m *Memory) GetScanNames(_ context.Context) ([]string, error) {
	return m.getNames(m.scans)
}

// GetScanPatterns implements store.Store.
func (m *Memory) GetScanPatterns(ctx context.Context, name string) ([]Pattern, error) {
	if m.Error() != nil {
		return nil, m.Error()
	}
	scan, _ := m.GetScan(ctx, name)
	return scan.Patterns, nil
}

// Init implements store.Store.
func (m *Memory) Init(_ context.Context) error {
	m.collections = &sync.Map{}
	m.histograms = &sync.Map{}
	m.scans = &sync.Map{}
	m.nodes = &sync.Map{}
	m.InjectError(nil)
	return nil
}

// InjectError sets the error that will be returned on each subsequent call.
func (m *Memory) InjectError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mu.err = err
}

// PutCollection implements store.Store.
func (m *Memory) PutCollection(_ context.Context, collection *Collection) error {
	if m.Error() != nil {
		return m.Error()
	}
	m.collections.Store(collection.Name, collection)
	return nil
}

// PutHistogram implements store.Store.
func (m *Memory) PutHistogram(_ context.Context, histogram *Histogram) error {
	if m.Error() != nil {
		return m.Error()
	}
	m.histograms.Store(histogram.Name, histogram)
	return nil
}

// PutScan implements store.Store.
func (m *Memory) PutScan(_ context.Context, scan *Scan) error {
	if m.Error() != nil {
		return m.Error()
	}
	m.scans.Store(scan.Name, scan)
	return nil
}

func (m *Memory) getNames(vals *sync.Map) ([]string, error) {
	if m.Error() != nil {
		return nil, m.Error()
	}
	names := make([]string, 0)
	vals.Range(func(key any, value any) bool {
		names = append(names, key.(string))
		return true
	})
	slices.Sort(names)
	return names, nil
}
