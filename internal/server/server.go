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

// Package server defines the common behavior of a process that reads a configuratio
// and runs a task.
package server

import "context"

// A Task can be started and stopped.
type Task interface {
	// Start the task
	Start(ctx context.Context) error
	// Stop the task, releasing all the resources associated with it.
	Stop(ctx context.Context) error
	Stopped() bool
}

// A Server is a task that has a configuration that can be refreshed.
type Server interface {
	Task
	// Refresh the server configuration
	Refresh(ctx context.Context) error
}
