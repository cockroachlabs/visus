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

// Package server implements a http server to export metrics in Prometheus format.
package server

import "context"

// A Server is a process that can be started and shutdown.
type Server interface {
	// Refresh the server configuration
	Refresh(ctx context.Context) error
	// Start the server
	Start(ctx context.Context) error
	// Shutdown the server, releasing all the resources associated with it.
	Shutdown(ctx context.Context) error
}
