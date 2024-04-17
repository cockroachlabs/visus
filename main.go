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

package main

import (
	"context"
	golog "log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cockroachlabs/visus/internal/cmd/collection"
	"github.com/cockroachlabs/visus/internal/cmd/histogram"
	"github.com/cockroachlabs/visus/internal/cmd/initialize"
	"github.com/cockroachlabs/visus/internal/cmd/scan"
	"github.com/cockroachlabs/visus/internal/cmd/server"
	joonix "github.com/joonix/log"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

//go:generate go run github.com/cockroachdb/crlfmt -w .
//go:generate go run golang.org/x/lint/golint -set_exit_status ./...
//go:generate go run honnef.co/go/tools/cmd/staticcheck -checks all ./...

func main() {
	var logFormat, logDestination string
	var verbosity int
	root := &cobra.Command{
		Use:           "visus",
		SilenceErrors: true,
		SilenceUsage:  true,
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
			// Hijack anything that uses the standard go logger, like http.
			pw := log.WithField("golog", true).Writer()
			log.DeferExitHandler(func() { _ = pw.Close() })
			// logrus will provide timestamp info.
			golog.SetFlags(0)
			golog.SetOutput(pw)

			switch verbosity {
			case 0:
			// No-op
			case 1:
				log.SetLevel(log.DebugLevel)
			default:
				log.SetLevel(log.TraceLevel)
			}

			switch logFormat {
			case "fluent":
				log.SetFormatter(joonix.NewFormatter())
			case "text":
				log.SetFormatter(&log.TextFormatter{
					FullTimestamp:   true,
					PadLevelText:    true,
					TimestampFormat: time.Stamp,
				})
			default:
				return errors.Errorf("unknown log format: %q", logFormat)
			}

			if logDestination != "" {
				f, err := os.OpenFile(logDestination, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
				if err != nil {
					log.WithError(err).Error("could not open log output file")
					log.Exit(1)
				}
				log.DeferExitHandler(func() { _ = f.Close() })
				log.SetOutput(f)
			}

			return nil
		},
	}
	f := root.PersistentFlags()
	f.StringVar(&logFormat, "logFormat", "text", "choose log output format [ fluent, text ]")
	f.StringVar(&logDestination, "logDestination", "", "write logs to a file, instead of stdout")
	f.CountVarP(&verbosity, "verbose", "v", "increase logging verbosity to debug; repeat for trace")

	root.AddCommand(server.Command())
	root.AddCommand(collection.Command())
	root.AddCommand(scan.Command())
	root.AddCommand(histogram.Command())
	root.AddCommand(initialize.Command())

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := root.ExecuteContext(ctx); err != nil {
		log.WithError(err).Error("could not execute command")
		log.Exit(1)
	}
	log.Exit(0)
}
