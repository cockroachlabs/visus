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
	"sync"
	"time"
)

// InMemory stores the configuration in memory. Used for testing.
type InMemory struct {
	MainNode    bool
	collections *sync.Map
	histograms  *sync.Map
}

var _ Store = &InMemory{}

// DeleteCollection implements store.Store.
func (m *InMemory) DeleteCollection(_ context.Context, name string) error {
	m.collections.Delete(name)
	return nil
}

// DeleteHistogram implements store.Store.
func (m *InMemory) DeleteHistogram(_ context.Context, name string) error {
	m.histograms.Delete(name)
	return nil
}

// GetCollection implements store.Store.
func (m *InMemory) GetCollection(_ context.Context, name string) (*Collection, error) {
	res, _ := m.collections.Load(name)
	return res.(*Collection), nil
}

// GetCollectionNames implements store.Store.
func (m *InMemory) GetCollectionNames(_ context.Context) ([]string, error) {
	return getNames(m.collections)
}

// GetHistogram implements store.Store.
func (m *InMemory) GetHistogram(_ context.Context, name string) (*Histogram, error) {
	res, _ := m.histograms.Load(name)
	return res.(*Histogram), nil
}

// GetHistogramNames implements store.Store.
func (m *InMemory) GetHistogramNames(_ context.Context) ([]string, error) {
	return getNames(m.histograms)
}

// GetMetrics implements store.Store.
func (m *InMemory) GetMetrics(ctx context.Context, name string) ([]Metric, error) {
	coll, _ := m.GetCollection(ctx, name)
	return coll.Metrics, nil
}

// Init implements store.Store.
func (m *InMemory) Init(_ context.Context) error {
	m.collections = &sync.Map{}
	m.histograms = &sync.Map{}
	return nil
}

// IsMainNode implements store.Store.
func (m *InMemory) IsMainNode(_ context.Context, lastUpdated time.Time) (bool, error) {
	return m.MainNode, nil
}

// PutCollection implements store.Store.
func (m *InMemory) PutCollection(_ context.Context, collection *Collection) error {
	m.collections.Store(collection.Name, collection)
	return nil
}

// PutHistogram implements store.Store.
func (m *InMemory) PutHistogram(_ context.Context, histogram *Histogram) error {
	m.histograms.Store(histogram.Name, histogram)
	return nil
}

func getNames(m *sync.Map) ([]string, error) {
	names := make([]string, 0)
	m.Range(func(key any, value any) bool {
		names = append(names, key.(string))
		return true
	})
	return names, nil
}
