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

// Package node defines the sub command to inspect visus node state.
package node

import (
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/cockroachlabs/visus/internal/cmd/env"
	"github.com/spf13/cobra"
)

const timeFmt = "2006-01-02 15:04:05"

var databaseURL = ""

// Command runs the node tools to view node and leader state.
func Command() *cobra.Command {
	return command(env.Default())
}

// command runs the node tools. An environment can be injected for standalone testing.
func command(env *env.Env) *cobra.Command {
	c := &cobra.Command{
		Use: "node",
	}
	f := c.PersistentFlags()
	c.AddCommand(listCmd(env))
	f.StringVar(&databaseURL, "url", "",
		"Connection URL, of the form: postgresql://[user[:passwd]@]host[:port]/[db][?parameters...]")
	return c
}

// listCmd lists all registered nodes and their leader status.
func listCmd(env *env.Env) *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Example: `./visus node list --url "postgresql://root@localhost:26257/defaultdb?sslmode=disable"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			store, err := env.ProvideStore(ctx, databaseURL)
			if err != nil {
				return errors.Wrap(err, "unable to connect to store")
			}
			nodes, err := store.GetNodes(ctx)
			if err != nil {
				return errors.Wrap(err, "unable to retrieve nodes")
			}
			zone, _ := time.Now().Zone()
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 1, ' ', 0)
			fmt.Fprintf(w, "ID\tHOSTNAME\tPID\tVERSION\tUPDATED (%s)\n", zone)
			for _, n := range nodes {
				fmt.Fprintf(w, "%d\t%s\t%d\t%s\t%s\n", n.ID, n.Hostname, n.PID, n.Version, n.Updated.Local().Format(timeFmt))
			}
			w.Flush()
			return nil
		},
	}
}
