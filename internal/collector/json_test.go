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

package collector

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_jsonCollector(t *testing.T) {
	coll := jsonCollector(nil, "intent", intentDef).(*collector)
	assert.Equal(t, 10, coll.frequency)
	assert.Equal(t, []string{"name"}, coll.labels)
	assert.Equal(t, "crdb_intent_count", coll.metrics[0].name)
	assert.Equal(t, Gauge, coll.metrics[0].kind)

	coll = jsonCollector(nil, "activity", activityDef).(*collector)
	assert.Equal(t, 5, coll.frequency)
	assert.Equal(t, []string{"statement_id", "application_name", "database_name"}, coll.labels)
	assert.Equal(t, "crdb_exec_count", coll.metrics[0].name)
	assert.Equal(t, Counter, coll.metrics[0].kind)
}
