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

// Package collection defines the sub command to run visus collection utilities.
package collection

import (
	"sort"
	"strings"
	"time"

	"github.com/cockroachlabs/visus/internal/cmd/env"
	"github.com/cockroachlabs/visus/internal/collector"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/expfmt"
	"github.com/spf13/cobra"
)

var (
	databaseURL           = ""
	microsecondsPerSecond = int64(1e6)
)

// Command runs the scan tools to view and manage the configuration in the database.
func Command() *cobra.Command {
	return command(env.Default())
}

// command runs the tools to view and manage the configuration in the database.
// An environment can be injected for standalone testing.
func command(env *env.Env) *cobra.Command {
	c := &cobra.Command{
		Use: "collection",
	}
	f := c.PersistentFlags()
	c.AddCommand(
		getCmd(env),
		listCmd(env),
		deleteCmd(env),
		putCmd(env),
		testCmd(env))
	f.StringVar(&databaseURL, "url", "",
		"Connection URL, of the form: postgresql://[user[:passwd]@]host[:port]/[db][?parameters...]")
	return c
}

// deleteCmd deletes a scan from the database.
func deleteCmd(env *env.Env) *cobra.Command {
	return &cobra.Command{
		Use:     "delete",
		Args:    cobra.ExactArgs(1),
		Example: `./visus collection delete collection_name  --url "postgresql://root@localhost:26257/defaultdb?sslmode=disable" `,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			collection := args[0]
			store, err := env.ProvideStore(ctx, databaseURL)
			if err != nil {
				return err
			}
			if err := store.DeleteCollection(ctx, collection); err != nil {
				return errors.Wrapf(err, "unable to delete collection %s", collection)
			}
			cmd.Printf("Collection %s deleted.\n", collection)
			return nil
		},
	}
}

// getCmd retrieves a collection configuration from the database.
func getCmd(env *env.Env) *cobra.Command {
	return &cobra.Command{
		Use:     "get",
		Args:    cobra.ExactArgs(1),
		Example: `./visus collection get collection_name  --url "postgresql://root@localhost:26257/defaultdb?sslmode=disable" `,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			name := args[0]
			store, err := env.ProvideStore(ctx, databaseURL)
			if err != nil {
				return err
			}
			collection, err := store.GetCollection(ctx, name)
			if err != nil {
				return errors.Wrapf(err, "unable to retrieve collection %s", name)
			}
			if collection == nil {
				cmd.Printf("Collection %s not found\n", name)
				return nil
			}
			res, err := marshal(collection)
			if err != nil {
				return errors.Wrapf(err, "unable to retrieve collection %s", name)
			}
			cmd.Print(string(res))
			return nil
		},
	}
}

// listCmd list all the collections in the database
func listCmd(env *env.Env) *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Example: `./visus collection list  --url "postgresql://root@localhost:26257/defaultdb?sslmode=disable" `,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			store, err := env.ProvideStore(ctx, databaseURL)
			if err != nil {
				return errors.Wrap(err, "unable to retrieve collections")
			}
			names, err := store.GetCollectionNames(ctx)
			if err != nil {
				return errors.Wrap(err, "unable to retrieve collections")
			}
			sort.Strings(names)
			for _, name := range names {
				cmd.Printf("%s\n", name)
			}
			return nil
		},
	}
}

// putCmd inserts a new collection in the database using the specified yaml configuration.
func putCmd(env *env.Env) *cobra.Command {
	var file string
	c := &cobra.Command{
		Use:     "put",
		Args:    cobra.ExactArgs(0),
		Example: `./visus collection put --yaml config.yaml --url "postgresql://root@localhost:26257/defaultdb?sslmode=disable" `,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if file == "" {
				return errors.New("yaml configuration required")
			}
			store, err := env.ProvideStore(ctx, databaseURL)
			if err != nil {
				return err
			}
			data, err := env.ReadFile(file)
			if err != nil {
				return errors.Wrap(err, "unable to read collection configuration")
			}
			collection, err := unmarshal(data)
			if err != nil {
				return errors.Wrap(err, "unable to read collection configuration")
			}
			if err := store.PutCollection(ctx, collection); err != nil {
				return errors.Wrapf(err, "unable to insert collection %s", collection.Name)
			}
			cmd.Printf("Collection %s inserted.\n", collection.Name)
			return nil
		},
	}
	f := c.Flags()
	f.StringVar(&file, "yaml", "", "file containing the configuration")
	return c
}

func testCmd(env *env.Env) *cobra.Command {
	var interval time.Duration
	var count int
	var allowUnsafeInternals bool
	c := &cobra.Command{
		Use:     "test",
		Example: `./visus collection test  collection_name  --url "postgresql://root@localhost:26257/defaultdb?sslmode=disable" `,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			collectionName := args[0]
			st, err := env.ProvideStore(ctx, databaseURL)
			if err != nil {
				return err
			}
			conn, err := env.ProvideReadOnlyConnection(ctx, databaseURL, allowUnsafeInternals)
			if err != nil {
				return err
			}
			coll, err := st.GetCollection(ctx, collectionName)
			if err != nil {
				return errors.Wrapf(err, "Error retrieving collection %s", collectionName)
			}
			if coll == nil {
				cmd.Printf("Collection %s not found\n", collectionName)
			} else {
				collector, err := collector.FromCollection(coll, prometheus.DefaultRegisterer)
				if err != nil {
					return errors.Wrapf(err, "Error creating collector for %s", collectionName)
				}
				for i := 1; i <= count || count == 0; i++ {
					if i > 1 {
						select {
						case <-ctx.Done():
							return ctx.Err()
						case <-time.After(interval):
						}
					}
					cmd.Printf("\n---- %s %s -----\n", time.Now().Format("01-02-2006 15:04:05"), coll.Name)
					collector.Collect(ctx, conn)
					gathering, err := prometheus.DefaultGatherer.Gather()
					if err != nil {
						return errors.Wrapf(err, "error collecting metrics for %s", collectionName)
					}
					for _, mf := range gathering {
						if strings.HasPrefix(*mf.Name, collectionName) {
							expfmt.MetricFamilyToText(cmd.OutOrStdout(), mf)
						}
					}

				}
			}
			return nil
		},
	}
	f := c.Flags()
	f.BoolVar(&allowUnsafeInternals, "allow-unsafe-internals", false, "set allow_unsafe_internals = true for read-only database connections")
	f.DurationVar(&interval, "interval", 10*time.Second, "interval of collection")
	f.IntVar(&count, "count", 1, "number of times to run the collection. Specify 0 for continuos collection")
	return c
}
