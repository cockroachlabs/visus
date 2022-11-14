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
	"errors"
	"fmt"
	"os"

	"github.com/cockroachlabs/visus/internal/database"
	"github.com/cockroachlabs/visus/internal/http"
	"github.com/cockroachlabs/visus/internal/store"
	"github.com/cockroachlabs/visus/internal/translator"
	"github.com/creasty/defaults"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var databaseURL = ""

type config struct {
	Enabled bool `default:"true"`
	Name    string
	Bins    int `default:"10"`
	Start   int `default:"1000000"`
	End     int `default:"20000000000"`
	Regex   string
}

func getCmd() *cobra.Command {
	c := &cobra.Command{
		Use:     "get",
		Args:    cobra.ExactArgs(1),
		Example: `./visus histogram get histogram_name  --url "postgresql://root@localhost:26257/defaultdb?sslmode=disable" `,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			name := args[0]
			conn, err := database.New(ctx, databaseURL)
			if err != nil {
				return err
			}
			store := store.New(conn)
			histogram, err := store.GetHistogram(ctx, name)
			if err != nil {
				fmt.Printf("Error retrieving histogram %s.", name)
				return err
			}
			if histogram == nil {
				fmt.Printf("Histogram %s not found\n", name)
			} else {
				out, err := yaml.Marshal(config{
					Enabled: histogram.Enabled,
					Name:    histogram.Name,
					Bins:    histogram.Bins,
					Start:   histogram.Start,
					End:     histogram.End,
					Regex:   histogram.Regex,
				})
				if err != nil {
					fmt.Println(err)
					return err
				}
				fmt.Println(string(out))
			}
			return nil
		},
	}
	return c
}

func listCmd() *cobra.Command {
	c := &cobra.Command{
		Use:     "list",
		Example: `./visus histogram list  --url "postgresql://root@localhost:26257/defaultdb?sslmode=disable" `,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			conn, err := database.New(ctx, databaseURL)
			if err != nil {
				return err
			}
			store := store.New(conn)
			names, err := store.GetHistogramNames(ctx)
			if err != nil {
				fmt.Print("Error retrieving collections")
				return err
			}
			for _, name := range names {
				fmt.Println(name)
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
			conn, err := database.New(ctx, databaseURL)
			if err != nil {
				return err
			}
			store := store.New(conn)
			err = store.DeleteHistogram(ctx, regex)
			if err != nil {
				fmt.Printf("Error deleting histogram %s.\n", regex)
				return err
			}
			fmt.Printf("Deleted %s histogram.\n", regex)
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
			conn, err := database.New(ctx, databaseURL)
			if err != nil {
				return err
			}
			store := store.New(conn)
			names, err := store.GetHistogramNames(ctx)
			if err != nil {
				fmt.Print("Error retrieving histograms")
				return err
			}
			translators := make([]translator.Translator, 0)
			for _, n := range names {
				h, err := store.GetHistogram(ctx, n)
				if err != nil {
					fmt.Print("Error retrieving histogram")
					return err
				}
				hnew, err := translator.New(*h)
				if err != nil {
					log.Fatal(err)
				}
				translators = append(translators, hnew)
			}
			metricsIn, err := http.ReadMetrics(ctx, prometheus, nil)
			if err != nil {
				log.Fatal(err)
			}
			return http.WriteMetrics(ctx, metricsIn, true, translators, os.Stdout)
		},
	}
	f := c.Flags()
	f.StringVar(&prometheus, "prometheus", "", "prometheus endpoint")
	return c
}

func putCmd() *cobra.Command {
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
			conn, err := database.New(ctx, databaseURL)
			if err != nil {
				return err
			}
			data, err := os.ReadFile(file)
			if err != nil {
				return err
			}
			config := &config{}
			err = yaml.Unmarshal(data, &config)
			if err != nil {
				return err
			}
			if err := defaults.Set(config); err != nil {
				return err
			}
			st := store.New(conn)
			err = st.PutHistogram(ctx, &store.Histogram{
				Enabled: config.Enabled,
				Name:    config.Name,
				Bins:    config.Bins,
				Start:   config.Start,
				End:     config.End,
				Regex:   config.Regex,
			})
			if err != nil {
				fmt.Printf("Error inserting histogram %s.", config.Name)
				return err
			}
			fmt.Printf("histogram %s inserted.\n", config.Name)
			return nil
		},
	}
	f := c.Flags()
	f.StringVar(&file, "yaml", "", "yaml config")
	return c
}

// Command runs the admin tools to view and manage the configuration in the database.
func Command() *cobra.Command {
	c := &cobra.Command{
		Use: "histogram",
	}
	f := c.PersistentFlags()
	c.AddCommand(getCmd(), listCmd(), deleteCmd(), putCmd(), testCmd())
	f.StringVar(&databaseURL, "url", "",
		"Connection URL, of the form: postgresql://[user[:passwd]@]host[:port]/[db][?parameters...]")
	return c
}
