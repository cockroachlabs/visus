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

package scan

import (
	"github.com/cockroachlabs/visus/internal/store"
	"github.com/creasty/defaults"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v3"
)

// patternDef yaml pattern definition
type patternDef struct {
	Help    string
	Name    string
	Exclude string
	Regex   string
}

// config yaml scan definition
type config struct {
	Enabled  bool
	Format   string
	Name     string
	Path     string
	Patterns []patternDef
}

func marshal(logFile *store.Scan) ([]byte, error) {
	patterns := make([]patternDef, 0)
	for _, m := range logFile.Patterns {
		metric := patternDef{
			Help:    m.Help,
			Name:    m.Name,
			Exclude: m.Exclude,
			Regex:   m.Regex,
		}
		patterns = append(patterns, metric)
	}
	config := &config{
		Enabled:  logFile.Enabled,
		Format:   string(logFile.Format),
		Name:     logFile.Name,
		Path:     logFile.Path,
		Patterns: patterns,
	}
	return yaml.Marshal(config)
}

func unmarshal(data []byte) (*store.Scan, error) {
	config := &config{}
	err := yaml.Unmarshal(data, &config)
	if err != nil {
		return nil, err
	}
	if err := defaults.Set(config); err != nil {
		return nil, err
	}
	patterns := make([]store.Pattern, 0)
	for _, p := range config.Patterns {
		pattern := store.Pattern{
			Name:    p.Name,
			Exclude: p.Exclude,
			Regex:   p.Regex,
			Help:    p.Help,
		}
		patterns = append(patterns, pattern)
	}
	if config.Name == "" {
		return nil, errors.New("name must be specified")
	}
	return &store.Scan{
		Enabled:  config.Enabled,
		Format:   store.LogFormat(config.Format),
		Path:     config.Path,
		Name:     config.Name,
		Patterns: patterns,
	}, nil
}
