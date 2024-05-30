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

package histogram

import (
	"errors"

	"github.com/cockroachlabs/visus/internal/store"
	"github.com/creasty/defaults"
	"gopkg.in/yaml.v3"
)

type config struct {
	Bins    int `default:"10"`
	Enabled bool
	End     int `default:"20000000000"`
	Name    string
	Regex   string
	Start   int `default:"1000000"`
}

func marshal(histogram *store.Histogram) ([]byte, error) {
	return yaml.Marshal(config{
		Bins:    histogram.Bins,
		Enabled: histogram.Enabled,
		End:     histogram.End,
		Name:    histogram.Name,
		Regex:   histogram.Regex,
		Start:   histogram.Start,
	})
}

func unmarshal(data []byte) (*store.Histogram, error) {
	config := &config{}
	err := yaml.Unmarshal(data, &config)
	if err != nil {
		return nil, err
	}
	if err := defaults.Set(config); err != nil {
		return nil, err
	}
	if config.Regex == "" {
		return nil, errors.New("regex must be specified")
	}
	if config.Name == "" {
		return nil, errors.New("name must be specified")
	}
	return &store.Histogram{
		Bins:    config.Bins,
		Enabled: config.Enabled,
		End:     config.End,
		Name:    config.Name,
		Regex:   config.Regex,
		Start:   config.Start,
	}, nil
}
