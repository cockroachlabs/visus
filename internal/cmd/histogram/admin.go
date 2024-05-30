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

// Package histogram defines the sub command to manage histogram configurations.
package histogram

import (
	"sort"

	"github.com/cockroachlabs/visus/internal/cmd/env"
	"github.com/cockroachlabs/visus/internal/metric"
	"github.com/cockroachlabs/visus/internal/translator"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

var databaseURL = ""

// Command runs the admin tools to view and manage the configuration in the database.
func Command() *cobra.Command {
	return command(env.Default())
}

// command runs the tools to view and manage the configuration in the database.
// An environment can be injected for standalone testing.
func command(env *env.Env) *cobra.Command {
	c := &cobra.Command{
		Use: "histogram",
	}
	f := c.PersistentFlags()
	c.AddCommand(
		deleteCmd(env),
		getCmd(env),
		listCmd(env),
		putCmd(env),
		testCmd(env))
	f.StringVar(&databaseURL, "url", "",
		"Connection URL, of the form: postgresql://[user[:passwd]@]host[:port]/[db][?parameters...]")
	return c
}

func deleteCmd(env *env.Env) *cobra.Command {
	c := &cobra.Command{
		Use:     "delete",
		Args:    cobra.ExactArgs(1),
		Example: `./visus histogram delete regex  --url "postgresql://root@localhost:26257/defaultdb?sslmode=disable" `,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			name := args[0]
			store, err := env.ProvideStore(ctx, databaseURL)
			if err != nil {
				return err
			}
			if err := store.DeleteHistogram(ctx, name); err != nil {
				return errors.Wrapf(err, "unable to delete histogram %s", name)
			}
			cmd.Printf("Histogram %s deleted.\n", name)
			return nil
		},
	}
	return c
}

func getCmd(env *env.Env) *cobra.Command {
	c := &cobra.Command{
		Use:     "get",
		Args:    cobra.ExactArgs(1),
		Example: `./visus histogram get histogram_name  --url "postgresql://root@localhost:26257/defaultdb?sslmode=disable" `,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			name := args[0]
			store, err := env.ProvideStore(ctx, databaseURL)
			if err != nil {
				return err
			}
			histogram, err := store.GetHistogram(ctx, name)
			if err != nil {
				return errors.Wrapf(err, "unable to retrieve histogram %s", name)
			}
			if histogram == nil {
				cmd.Printf("Histogram %s not found\n", name)
				return nil
			}
			res, err := marshal(histogram)
			if err != nil {
				return errors.Wrapf(err, "unable to retrieve histogram %s", name)
			}
			cmd.Print(string(res))
			return nil
		},
	}
	return c
}

func listCmd(env *env.Env) *cobra.Command {
	c := &cobra.Command{
		Use:     "list",
		Example: `./visus histogram list  --url "postgresql://root@localhost:26257/defaultdb?sslmode=disable" `,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			store, err := env.ProvideStore(ctx, databaseURL)
			if err != nil {
				return errors.Wrap(err, "unable to retrieve histograms")
			}
			histograms, err := store.GetHistogramNames(ctx)
			if err != nil {
				return errors.Wrap(err, "unable to retrieve histograms")
			}
			sort.Strings(histograms)
			for _, h := range histograms {
				cmd.Printf("%s\n", h)
			}
			return nil
		},
	}
	return c
}

func putCmd(env *env.Env) *cobra.Command {
	var file string
	c := &cobra.Command{
		Use:     "put",
		Args:    cobra.NoArgs,
		Example: `./visus histogram put -yaml config  --url "postgresql://root@localhost:26257/defaultdb?sslmode=disable" `,
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
				return errors.Wrap(err, "unable to read histogram configuration")
			}
			histogram, err := unmarshal(data)
			if err != nil {
				return errors.Wrap(err, "unable to read histogram configuration")
			}
			if err := store.PutHistogram(ctx, histogram); err != nil {
				return errors.Wrapf(err, "unable to insert histogram %s", histogram.Name)
			}
			cmd.Printf("Histogram %s inserted.\n", histogram.Name)
			return nil
		},
	}
	f := c.Flags()
	f.StringVar(&file, "yaml", "", "yaml config")
	return c
}

func testCmd(env *env.Env) *cobra.Command {
	var prometheus string
	c := &cobra.Command{
		Use: "test",
		Example: `./visus histogram test 
		--prometheus http://localhost:8080/_status/vars  
		--url "postgresql://root@localhost:26257/defaultdb?sslmode=disable"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			store, err := env.ProvideStore(ctx, databaseURL)
			if err != nil {
				return errors.Wrap(err, "unable to retrieve histograms")
			}
			translators, err := translator.Load(ctx, store)
			if err != nil {
				return errors.Wrap(err, "unable to retrieve histograms")
			}
			if len(translators) == 0 {
				return errors.New("found no histograms")
			}
			writer, err := metric.NewWriter(prometheus, nil, translators)
			if err != nil {
				return errors.Wrap(err, "unable to collect metrics")
			}
			return writer.Copy(ctx, cmd.OutOrStdout())
		},
	}
	f := c.Flags()
	f.StringVar(&prometheus, "prometheus", "", "prometheus endpoint")
	return c
}
