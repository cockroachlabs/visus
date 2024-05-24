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

// Package scan defines the sub command to run visus scan utilities.
package scan

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/cockroachlabs/visus/internal/database"
	"github.com/cockroachlabs/visus/internal/scanner"
	"github.com/cockroachlabs/visus/internal/stopper"
	"github.com/cockroachlabs/visus/internal/store"
	"github.com/creasty/defaults"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/expfmt"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var databaseURL = ""

// patternDef yaml pattern definition
type patternDef struct {
	Help  string
	Name  string
	Regex string
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
			Help:  m.Help,
			Name:  m.Name,
			Regex: m.Regex,
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

// listCmd list all the log scans in the database
func listCmd() *cobra.Command {
	c := &cobra.Command{
		Use:     "list",
		Example: `./visus scan list  --url "postgresql://root@localhost:26257/defaultdb?sslmode=disable" `,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			conn, err := database.New(ctx, databaseURL)
			if err != nil {
				return err
			}
			store := store.New(conn)
			logs, err := store.GetScanNames(ctx)
			if err != nil {
				fmt.Print("Error retrieving log targets")
				return err
			}
			for _, lg := range logs {
				fmt.Printf("%s\n", lg)
			}
			return nil
		},
	}
	return c
}

// getCmd retrieves a scan configuration from the database.
func getCmd() *cobra.Command {
	c := &cobra.Command{
		Use:     "get",
		Args:    cobra.ExactArgs(1),
		Example: `./visus scan get scan_name  --url "postgresql://root@localhost:26257/defaultdb?sslmode=disable" `,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			logName := args[0]
			conn, err := database.New(ctx, databaseURL)
			if err != nil {
				return err
			}
			store := store.New(conn)
			scanner, err := store.GetScan(ctx, logName)
			if err != nil {
				fmt.Printf("Error retrieving log %s.", logName)
				return err
			}
			if scanner == nil {
				fmt.Printf("Scan %s not found\n", logName)
			} else {
				res, err := marshal(scanner)
				if err != nil {
					fmt.Printf("Unabled to marshall %s\n", logName)
				}
				fmt.Println(string(res))
			}
			return nil
		},
	}
	return c
}

// testCmd retrieves a scan configuration and execute it, returning the metrics extracted
// from the log file in the scan.
func testCmd() *cobra.Command {
	var interval time.Duration
	var count int
	c := &cobra.Command{
		Use:     "test",
		Args:    cobra.ExactArgs(1),
		Example: `./visus scan test scan_name  --url "postgresql://root@localhost:26257/defaultdb?sslmode=disable" `,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := stopper.WithContext(cmd.Context())
			logName := args[0]
			conn, err := database.ReadOnly(ctx, databaseURL)
			if err != nil {
				return err
			}
			st := store.New(conn)
			lg, err := st.GetScan(ctx, logName)
			if err != nil {
				fmt.Printf("Error retrieving scan %s.", logName)
				return err
			}
			if lg == nil {
				fmt.Printf("Scan %s not found\n", logName)
			} else {
				scanner, err := scanner.FromConfig(lg,
					&scanner.Config{
						FromBeginning: true,
						Poll:          true,
						Follow:        false,
					},
					prometheus.DefaultRegisterer)
				if err != nil {
					return err
				}
				scanner.Start(ctx)
				for i := 1; i <= count || count == 0; i++ {
					select {
					case <-ctx.Done():
						return ctx.Err()
					case <-time.After(interval):
					}
					gathering, err := prometheus.DefaultGatherer.Gather()
					if err != nil {
						fmt.Printf("Error retrieving patterns %s.", err)
						return err
					}
					fmt.Printf("\n---- %s %s -----\n", time.Now().Format("01-02-2006 15:04:05"), lg.Name)
					for _, mf := range gathering {
						if strings.HasPrefix(*mf.Name, logName) {
							expfmt.MetricFamilyToText(os.Stdout, mf)

						}
					}
				}
				return scanner.Stop()
			}
			return nil
		},
	}
	f := c.Flags()
	f.DurationVar(&interval, "interval", 10*time.Second, "interval of scan")
	f.IntVar(&count, "count", 1, "number of times to run the scan. Specify 0 for continuos scan")
	return c
}

// deleteCmd deletes a scan from the database.
func deleteCmd() *cobra.Command {
	c := &cobra.Command{
		Use:     "delete",
		Args:    cobra.ExactArgs(1),
		Example: `./visus scan delete scan_name  --url "postgresql://root@localhost:26257/defaultdb?sslmode=disable" `,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			logName := args[0]
			conn, err := database.New(ctx, databaseURL)
			if err != nil {
				return err
			}
			store := store.New(conn)
			err = store.DeleteScan(ctx, args[0])
			if err != nil {
				fmt.Printf("Error deleting scan %s.\n", logName)
				return err
			}
			fmt.Printf("Scan %s deleted.\n", logName)
			return nil
		},
	}
	return c
}

// putCmd inserts a new scan in the database using the specified yaml configuration.
func putCmd() *cobra.Command {
	var file string
	c := &cobra.Command{
		Use:     "put",
		Args:    cobra.ExactArgs(0),
		Example: `./visus scan put --yaml config.yaml --url "postgresql://root@localhost:26257/defaultdb?sslmode=disable" `,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if file == "" {
				return errors.New("yaml configuration required")
			}
			conn, err := database.New(ctx, databaseURL)
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
			patterns := make([]store.Pattern, 0)
			for _, p := range config.Patterns {
				pattern := store.Pattern{
					Name:  p.Name,
					Regex: p.Regex,
					Help:  p.Help,
				}
				patterns = append(patterns, pattern)
			}
			if config.Name == "" {
				return errors.New("name must be specified")
			}
			logTarget := &store.Scan{
				Enabled:  config.Enabled,
				Format:   store.LogFormat(config.Format),
				Path:     config.Path,
				Name:     config.Name,
				Patterns: patterns,
			}
			store := store.New(conn)
			err = store.PutScan(ctx, logTarget)
			if err != nil {
				fmt.Printf("Error inserting scan %s.", config.Name)
				return err
			}
			fmt.Printf("Scan %s inserted.\n", config.Name)
			return nil
		},
	}
	f := c.Flags()
	f.StringVar(&file, "yaml", "", "file containing the configuration")
	return c
}

// Command runs the scan tools to view and manage the configuration in the database.
func Command() *cobra.Command {
	c := &cobra.Command{
		Use: "scan",
	}
	f := c.PersistentFlags()
	c.AddCommand(
		getCmd(),
		listCmd(),
		deleteCmd(),
		putCmd(),
		testCmd())
	f.StringVar(&databaseURL, "url", "",
		"Connection URL, of the form: postgresql://[user[:passwd]@]host[:port]/[db][?parameters...]")
	return c
}
