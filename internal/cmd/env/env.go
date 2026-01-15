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

// Package env provides the environment for the commands to run.
package env

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"os"

	"github.com/cockroachlabs/visus/internal/database"
	"github.com/cockroachlabs/visus/internal/store"
	"github.com/spf13/cobra"
)

// Env defines the environment to run the commands.
type Env struct {
	alt     io.Reader
	roPool  database.Connection
	store   store.Store
	testing bool
}

// Default returns the default environment, for real world use.
func Default() *Env {
	return &Env{
		alt: os.Stdin,
	}
}

// Testing sets up an environment for testing commands.
func Testing(ctx context.Context) *Env {
	store := &store.Memory{}
	err := store.Init(ctx)
	if err != nil {
		// this shouldn't happen for in memory store.
		panic("failed to initialize the store")
	}
	return &Env{
		testing: true,
		store:   store,
	}
}

// InjectMockConnection sets a mock connection for testing.
func (e *Env) InjectMockConnection(pool database.Connection) {
	e.roPool = pool
}

// InjectReader sets the alternative reader to use in the env.
func (e *Env) InjectReader(r io.Reader) {
	if e.testing {
		e.alt = r
	}
}

// InjectStoreError forces the in memory store to return an error.
func (e *Env) InjectStoreError(err error) {
	if mem, ok := e.store.(*store.Memory); e.testing && ok {
		mem.InjectError(err)
	}
}

// ProvideReadOnlyConnection returns the database connection to use for collectors.
func (e *Env) ProvideReadOnlyConnection(
	ctx context.Context, databaseURL string, allowUnsafeInternals bool,
) (database.Connection, error) {
	if e.testing {
		return e.roPool, nil
	}
	var err error
	e.roPool, err = database.ReadOnly(ctx, databaseURL, allowUnsafeInternals)
	if err != nil {
		return nil, err
	}
	return e.roPool, nil
}

// ProvideStore returns the Store to use.
func (e *Env) ProvideStore(ctx context.Context, databaseURL string) (store.Store, error) {
	if e.testing {
		return e.store, nil
	}
	conn, err := database.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	e.store = store.New(conn)
	return e.store, nil
}

// ReadFile reads the named file. If the filename is "-", it will read
// the content from the alternate reader in the environment.
func (e *Env) ReadFile(file string) ([]byte, error) {
	var data []byte
	if file == "-" {
		var buffer bytes.Buffer
		scanner := bufio.NewScanner(e.alt)
		for scanner.Scan() {
			buffer.Write(scanner.Bytes())
			buffer.WriteString("\n")
		}
		if err := scanner.Err(); err != nil {
			return nil, err
		}
		data = buffer.Bytes()
		return data, nil
	}
	return os.ReadFile(file)
}

// TestCommand executes the given command with the supplied arguments
// and returns the standard output as a string.
func (e *Env) TestCommand(f func(*Env) *cobra.Command, args []string) (string, error) {
	cmd := f(e)
	buffer := bytes.NewBufferString("")
	cmd.SetOut(buffer)
	cmd.SetArgs(args)
	err := cmd.Execute()
	if err != nil {
		return "", err
	}
	out, err := io.ReadAll(buffer)
	return string(out), err
}
