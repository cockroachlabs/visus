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
	"sort"
	"strings"
	"time"

	"github.com/cockroachlabs/visus/internal/cmd/env"
	"github.com/cockroachlabs/visus/internal/scanner"
	"github.com/cockroachlabs/visus/internal/stopper"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/expfmt"
	"github.com/spf13/cobra"
)

var databaseURL = ""

// Command runs the scan tools to view and manage the configuration in the database.
func Command() *cobra.Command {
	return command(env.Default())
}

// command runs the tools to view and manage the configuration in the database.
// An environment can be injected for standalone testing.
func command(env *env.Env) *cobra.Command {
	c := &cobra.Command{
		Use: "scan",
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
		Example: `./visus scan delete scan_name  --url "postgresql://root@localhost:26257/defaultdb?sslmode=disable" `,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			scan := args[0]
			store, err := env.ProvideStore(ctx, databaseURL)
			if err != nil {
				return err
			}
			if err := store.DeleteScan(ctx, scan); err != nil {
				return errors.Wrapf(err, "unable to delete scan %s", scan)
			}
			cmd.Printf("Scan %s deleted.\n", scan)
			return nil
		},
	}
}

// getCmd retrieves a scan configuration from the database.
func getCmd(env *env.Env) *cobra.Command {
	return &cobra.Command{
		Use:     "get",
		Args:    cobra.ExactArgs(1),
		Example: `./visus scan get scan_name  --url "postgresql://root@localhost:26257/defaultdb?sslmode=disable" `,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			scanName := args[0]
			store, err := env.ProvideStore(ctx, databaseURL)
			if err != nil {
				return err
			}
			scan, err := store.GetScan(ctx, scanName)
			if err != nil {
				return errors.Wrapf(err, "unable to retrieve scan %s", scanName)
			}
			if scan == nil {
				cmd.Printf("Scan %s not found\n", scanName)
				return nil
			}
			res, err := marshal(scan)
			if err != nil {
				return errors.Wrapf(err, "unable to retrieve scan %s", scanName)
			}
			cmd.Print(string(res))
			return nil
		},
	}
}

// listCmd list all the log scans in the database
func listCmd(env *env.Env) *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Example: `./visus scan list  --url "postgresql://root@localhost:26257/defaultdb?sslmode=disable" `,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			store, err := env.ProvideStore(ctx, databaseURL)
			if err != nil {
				return errors.Wrap(err, "unable to retrieve scans")
			}
			scans, err := store.GetScanNames(ctx)
			if err != nil {
				return errors.Wrap(err, "unable to retrieve scans")
			}
			sort.Strings(scans)
			for _, scan := range scans {
				cmd.Printf("%s\n", scan)
			}
			return nil
		},
	}
}

// putCmd inserts a new scan in the database using the specified yaml configuration.
func putCmd(env *env.Env) *cobra.Command {
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
			store, err := env.ProvideStore(ctx, databaseURL)
			if err != nil {
				return err
			}
			data, err := env.ReadFile(file)
			if err != nil {
				return errors.Wrap(err, "unable to read read configuration")
			}
			scan, err := unmarshal(data)
			if err != nil {
				return errors.Wrap(err, "unable to read scan configuration")
			}
			if err := store.PutScan(ctx, scan); err != nil {
				return errors.Wrapf(err, "unable to insert scan %s", scan.Name)
			}
			cmd.Printf("Scan %s inserted.\n", scan.Name)
			return nil
		},
	}
	f := c.Flags()
	f.StringVar(&file, "yaml", "", "file containing the configuration")
	return c
}

// testCmd retrieves a scan configuration and execute it, returning the metrics extracted
// from the log file in the scan.
func testCmd(env *env.Env) *cobra.Command {
	var interval time.Duration
	var count int
	c := &cobra.Command{
		Use:     "test",
		Args:    cobra.ExactArgs(1),
		Example: `./visus scan test scan_name  --url "postgresql://root@localhost:26257/defaultdb?sslmode=disable" `,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := stopper.WithContext(cmd.Context())

			scanName := args[0]
			store, err := env.ProvideStore(ctx, databaseURL)
			if err != nil {
				return err
			}
			scan, err := store.GetScan(ctx, scanName)
			if err != nil {
				return errors.Wrapf(err, "unable to retrieve scan %s", scanName)
			}
			if scan == nil {
				cmd.Printf("Scan %s not found\n", scanName)
				return nil
			}

			scanner, err := scanner.FromConfig(scan,
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
					return errors.Wrap(err, "prometheus failure")
				}
				cmd.Printf("\n---- %s %s -----\n", time.Now().Format("01-02-2006 15:04:05"), scan.Name)
				for _, mf := range gathering {
					if strings.HasPrefix(*mf.Name, scan.Name) {
						expfmt.MetricFamilyToText(cmd.OutOrStdout(), mf)

					}
				}
			}
			return scanner.Stop()
		},
	}
	f := c.Flags()
	f.DurationVar(&interval, "interval", 10*time.Second, "interval of scan")
	f.IntVar(&count, "count", 1, "number of times to run the scan. Specify 0 for continuos scan")
	return c
}
