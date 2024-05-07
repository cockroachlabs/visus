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

// Package initialize defines the sub command to initialize the _visus database.
package initialize

import (
	"fmt"

	"github.com/cockroachlabs/visus/internal/database"
	"github.com/cockroachlabs/visus/internal/store"
	"github.com/spf13/cobra"
)

var (
	databaseURL = ""
)

// Command inits the database.
func Command() *cobra.Command {
	c := &cobra.Command{
		Use:     "init",
		Example: `./visus init  --url "postgresql://root@localhost:26257/defaultdb?sslmode=disable" `,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			conn, err := database.New(ctx, databaseURL)
			if err != nil {
				return err
			}
			store := store.New(conn)
			err = store.Init(ctx)
			if err != nil {
				fmt.Printf("Error initializing database at %s.\n", databaseURL)
				return err
			}
			fmt.Printf("Database initialized at %s\n", databaseURL)
			return nil
		},
	}
	f := c.PersistentFlags()
	f.StringVar(&databaseURL, "url", "",
		"Connection URL, of the form: postgresql://[user[:passwd]@]host[:port]/[db][?parameters...]")
	return c
}
