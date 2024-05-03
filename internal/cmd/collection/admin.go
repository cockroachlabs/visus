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

// Package collection defines the sub command to run visus collection utilities.
package collection

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	"github.com/cockroachlabs/visus/internal/collector"
	"github.com/cockroachlabs/visus/internal/database"
	"github.com/cockroachlabs/visus/internal/store"
	"github.com/creasty/defaults"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/expfmt"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var (
	databaseURL           = ""
	microsecondsPerSecond = int64(math.Pow10(6))
)

// metricDef yaml metric definition
type metricDef struct {
	Name string
	Kind string
	Help string
}

// config yaml collection definition
type config struct {
	Name       string
	Frequency  int
	MaxResults int
	Enabled    bool
	Query      string
	Labels     []string
	Metrics    []metricDef
	Scope      string `default:"node"`
}

func marshal(collection *store.Collection) ([]byte, error) {
	metrics := make([]metricDef, 0)
	for _, m := range collection.Metrics {
		metric := metricDef{
			Name: m.Name,
			Kind: string(m.Kind),
			Help: m.Help,
		}
		metrics = append(metrics, metric)
	}
	config := &config{
		Name:       collection.Name,
		Frequency:  int(collection.Frequency.Microseconds / microsecondsPerSecond),
		MaxResults: collection.MaxResult,
		Enabled:    collection.Enabled,
		Query:      collection.Query,
		Labels:     collection.Labels,
		Metrics:    metrics,
		Scope:      string(collection.Scope),
	}
	return yaml.Marshal(config)
}

// listCmd list all the collections in the datababse
func listCmd(factory database.Factory) *cobra.Command {
	c := &cobra.Command{
		Use:     "list",
		Example: `./visus collection list  --url "postgresql://root@localhost:26257/defaultdb?sslmode=disable" `,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			conn, err := factory.New(ctx, databaseURL)
			if err != nil {
				return err
			}
			store := store.New(conn)
			collections, err := store.GetCollectionNames(ctx)
			if err != nil {
				fmt.Print("Error retrieving collections")
				return err
			}
			for _, coll := range collections {
				fmt.Printf("%s\n", coll)
			}
			return nil
		},
	}
	return c
}

func getCmd(factory database.Factory) *cobra.Command {
	c := &cobra.Command{
		Use:     "get",
		Args:    cobra.ExactArgs(1),
		Example: `./visus collection get collection_name  --url "postgresql://root@localhost:26257/defaultdb?sslmode=disable" `,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			collectionName := args[0]
			conn, err := factory.New(ctx, databaseURL)
			if err != nil {
				return err
			}
			store := store.New(conn)
			collection, err := store.GetCollection(ctx, collectionName)
			if err != nil {
				fmt.Printf("Error retrieving collection %s.", collectionName)
				return err
			}
			if collection == nil {
				fmt.Printf("Collection %s not found\n", collectionName)
			} else {
				res, err := marshal(collection)
				if err != nil {
					fmt.Printf("Unabled to marshall %s\n", collectionName)
				}
				fmt.Println(string(res))
			}
			return nil
		},
	}
	return c
}

func testCmd(factory database.Factory) *cobra.Command {
	var interval time.Duration
	var count int
	c := &cobra.Command{
		Use:     "test",
		Args:    cobra.ExactArgs(1),
		Example: `./visus collection test collection_name  --url "postgresql://root@localhost:26257/defaultdb?sslmode=disable" `,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			collectionName := args[0]
			conn, err := factory.ReadOnly(ctx, databaseURL)
			if err != nil {
				return err
			}
			st := store.New(conn)
			coll, err := st.GetCollection(ctx, collectionName)
			if err != nil {
				fmt.Printf("Error retrieving collection %s.", collectionName)
				return err
			}
			if coll == nil {
				fmt.Printf("Collection %s not found\n", collectionName)
			} else {
				collector := collector.New(coll.Name, coll.Labels, coll.Query).
					WithMaxResults(coll.MaxResult)
				for _, m := range coll.Metrics {
					switch m.Kind {
					case store.Gauge:
						collector.AddGauge(m.Name, m.Help)
					case store.Counter:
						collector.AddCounter(m.Name, m.Help)
					}
				}
				for i := 1; i <= count || count == 0; i++ {
					if i > 1 {
						select {
						case <-ctx.Done():
							return ctx.Err()
						case <-time.After(interval):
						}
					}
					fmt.Printf("\n---- %s %s -----\n", time.Now().Format("01-02-2006 15:04:05"), coll.Name)
					collector.Collect(ctx, conn)
					gathering, err := prometheus.DefaultGatherer.Gather()
					if err != nil {
						fmt.Printf("Error collecting metrics %s.", err)
						return err
					}
					for _, mf := range gathering {
						if strings.HasPrefix(*mf.Name, collectionName) {
							expfmt.MetricFamilyToText(os.Stdout, mf)

						}
					}

				}
			}
			return nil
		},
	}
	f := c.Flags()
	f.DurationVar(&interval, "interval", 10*time.Second, "interval of collection")
	f.IntVar(&count, "count", 1, "number of times to run the collection. Specify 0 for continuos collection")
	return c
}

func deleteCmd(factory database.Factory) *cobra.Command {
	c := &cobra.Command{
		Use:     "delete",
		Args:    cobra.ExactArgs(1),
		Example: `./visus collection delete collection_name  --url "postgresql://root@localhost:26257/defaultdb?sslmode=disable" `,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			collectionName := args[0]
			conn, err := factory.New(ctx, databaseURL)
			if err != nil {
				return err
			}
			store := store.New(conn)
			err = store.DeleteCollection(ctx, args[0])
			if err != nil {
				fmt.Printf("Error deleting collection %s.\n", collectionName)
				return err
			}
			fmt.Printf("Collection %s deleted.\n", collectionName)
			return nil
		},
	}
	return c
}

func putCmd(factory database.Factory) *cobra.Command {
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
			conn, err := factory.New(ctx, databaseURL)
			if err != nil {
				return err
			}
			var data []byte
			if file == "-" {
				var buffer bytes.Buffer
				scanner := bufio.NewScanner(os.Stdin)
				for scanner.Scan() {
					buffer.Write(scanner.Bytes())
					buffer.WriteString("\n")
				}
				if err := scanner.Err(); err != nil {
					log.Errorf("reading standard input: %s", err.Error())
				}
				data = buffer.Bytes()
			} else {
				data, err = os.ReadFile(file)
				if err != nil {
					return err
				}
			}
			config := &config{}
			err = yaml.Unmarshal(data, &config)
			if err != nil {
				return err
			}
			if err := defaults.Set(config); err != nil {
				return err
			}
			metrics := make([]store.Metric, 0)
			for _, m := range config.Metrics {
				metrics = append(metrics, store.Metric{
					Name: m.Name,
					Kind: store.Kind(m.Kind),
					Help: m.Help,
				})
			}
			var scope store.Scope
			switch strings.ToLower(config.Scope) {
			case "node":
				scope = store.Node
			case "cluster":
				scope = store.Cluster
			default:
				return errors.New("invalid scope")
			}
			if config.Query == "" {
				return errors.New("query must be specified")
			}
			if config.Name == "" {
				return errors.New("name must be specified")
			}
			collection := &store.Collection{
				Name:      config.Name,
				Enabled:   config.Enabled,
				Scope:     scope,
				MaxResult: config.MaxResults,
				Frequency: pgtype.Interval{
					Valid:        true,
					Microseconds: int64(config.Frequency) * microsecondsPerSecond,
				},
				Query:   config.Query,
				Labels:  config.Labels,
				Metrics: metrics,
			}
			store := store.New(conn)
			err = store.PutCollection(ctx, collection)
			if err != nil {
				fmt.Printf("Error inserting collection %s.\n", config.Name)
				return err
			}
			fmt.Printf("Collection %s inserted.\n", config.Name)
			return nil
		},
	}
	f := c.Flags()
	f.StringVar(&file, "yaml", "", "file containing the configuration")
	return c
}

// Command runs the collection tools to view and manage the configuration in the database.
func Command() *cobra.Command {
	c := &cobra.Command{
		Use: "collection",
	}
	f := c.PersistentFlags()
	c.AddCommand(
		getCmd(database.DefaultFactory),
		listCmd(database.DefaultFactory),
		deleteCmd(database.DefaultFactory),
		putCmd(database.DefaultFactory),
		testCmd(database.DefaultFactory))
	f.StringVar(&databaseURL, "url", "",
		"Connection URL, of the form: postgresql://[user[:passwd]@]host[:port]/[db][?parameters...]")
	return c
}
