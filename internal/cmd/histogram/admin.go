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

// Package histogram defines the sub command to manage histogram configurations.
package histogram

import (
	"fmt"
	"log"
	"os"

	"github.com/cockroachlabs/visus/internal/database"
	"github.com/cockroachlabs/visus/internal/http"
	"github.com/cockroachlabs/visus/internal/store"
	"github.com/cockroachlabs/visus/internal/translator"
	"github.com/spf13/cobra"
)

// params used by the put command to store a new configuration in the database.
type params struct {
	bins    int
	disable bool
	end     int
	start   int
}

var databaseURL = ""

func listCmd() *cobra.Command {
	c := &cobra.Command{
		Use:     "list",
		Example: `./visus histogram list  --url "postgresql://root@localhost:26257/defaultdb?sslmode=disable" `,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			pool, err := database.New(ctx, databaseURL)
			if err != nil {
				return err
			}
			store := store.New(pool)
			histograms, err := store.GetHistograms(ctx)
			if err != nil {
				fmt.Print("Error retrieving collections")
				return err
			}
			for _, histogram := range histograms {
				fmt.Printf("Histogram: %+v\n", histogram)
			}
			return nil
		},
	}
	return c
}

func deleteCmd() *cobra.Command {
	c := &cobra.Command{
		Use:     "delete",
		Args:    cobra.ExactArgs(1),
		Example: `./visus histogram delete regex  --url "postgresql://root@localhost:26257/defaultdb?sslmode=disable" `,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			regex := args[0]
			pool, err := database.New(ctx, databaseURL)
			if err != nil {
				return err
			}
			store := store.New(pool)
			err = store.DeleteHistogram(ctx, regex)
			if err != nil {
				fmt.Printf("Error deleting histogram %s.\n", regex)
				return err
			}
			fmt.Printf("Collection %s histogram.\n", regex)
			return nil
		},
	}
	return c
}

func testCmd() *cobra.Command {
	var prometheus string
	c := &cobra.Command{
		Use:     "test",
		Example: `./visus histogram test --prometheus http://localhost:8080/_status/vars  --url "postgresql://root@localhost:26257/defaultdb?sslmode=disable" `,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			pool, err := database.New(ctx, databaseURL)
			if err != nil {
				return err
			}
			store := store.New(pool)
			histograms, err := store.GetHistograms(ctx)
			if err != nil {
				fmt.Print("Error retrieving collections")
				return err
			}
			translators := make([]translator.Translator, 0)
			for _, h := range histograms {
				hnew, err := translator.New(h)
				if err != nil {
					log.Fatal(err)
				}
				translators = append(translators, hnew)
			}
			metricsIn, err := http.ReadMetrics(ctx, prometheus, nil)
			if err != nil {
				log.Fatal(err)
			}
			return http.WriteMetrics(ctx, metricsIn, translators, os.Stdout)
		},
	}
	f := c.Flags()
	f.StringVar(&prometheus, "prometheus", "", "prometheus endpoint")
	return c
}

func putCmd() *cobra.Command {
	params := &params{}
	c := &cobra.Command{
		Use:     "put",
		Args:    cobra.ExactArgs(1),
		Example: `./visus histogram put "^sql_exec_latency$"  --url "postgresql://root@localhost:26257/defaultdb?sslmode=disable" `,
		RunE: func(cmd *cobra.Command, args []string) error {
			regex := args[0]
			ctx := cmd.Context()
			pool, err := database.New(ctx, databaseURL)
			if err != nil {
				return err
			}
			histogram := &store.Histogram{
				Bins:    params.bins,
				Enabled: !params.disable,
				End:     params.end,
				Regex:   regex,
				Start:   params.start,
			}
			store := store.New(pool)
			err = store.PutHistogram(ctx, histogram)
			if err != nil {
				fmt.Printf("Error inserting histogram %s.", regex)
				return err
			}
			fmt.Printf("histogram %s inserted.\n", regex)
			return nil
		},
	}
	f := c.Flags()
	f.BoolVar(&params.disable, "disable", false, "disable the rewriting rule")
	f.IntVar(&params.bins, "bins", 10, "number of linear bins")
	f.IntVar(&params.start, "start", 1000000, "histogram starting value")
	f.IntVar(&params.end, "end", 20000000000, "histogram ending value")
	return c
}

// Command runs the admin tools to view and manage the configuration in the database.
func Command() *cobra.Command {
	c := &cobra.Command{
		Use: "histogram",
	}
	f := c.PersistentFlags()
	c.AddCommand(listCmd(), deleteCmd(), putCmd(), testCmd())
	f.StringVar(&databaseURL, "url", "",
		"Connection URL, of the form: postgresql://[user[:passwd]@]host[:port]/[db][?parameters...]")
	return c
}
