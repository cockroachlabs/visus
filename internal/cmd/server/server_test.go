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

package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHelp ensures that the CLI command can be constructed and
// that all flag binding works.
func TestHelp(t *testing.T) {
	r := require.New(t)
	r.NoError(Command().Help())
}

// TestFlagValidation verifies that the two flag-consistency checks at the
// top of RunE fire before any database connection is attempted, since both
// return before dialing the URL.
func TestFlagValidation(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "missing certs without insecure",
			args:    []string{},
			wantErr: "--insecure must be specfied if certificates and private key are missing",
		},
		{
			name:    "missing url",
			args:    []string{"--insecure"},
			wantErr: "--url must be specified",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := assert.New(t)
			cmd := Command()
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true
			cmd.SetArgs(tt.args)
			a.ErrorContains(cmd.Execute(), tt.wantErr)
		})
	}
}
