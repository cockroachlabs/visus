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
	"encoding/json"
	"strings"

	"github.com/cockroachlabs/visus/internal/database"
	log "github.com/sirupsen/logrus"
)

type MetricConfig struct {
	Type string
	Name string
	Desc string
}
type CollectorConfig struct {
	Name       string
	Query      []string
	MaxResults int64
	Frequency  int64
	Labels     []string
	Metrics    []MetricConfig
}

const (
	activityDef = `
	{
		"name": "activity",
		"query": [
			"SELECT",
			"   statement_id as label_statement,",
			"   application_name,",
			"   database_name,",
			"   sum(count) AS exec_count,",
			"   max(max_disk_usage_avg) AS max_disk,",
			"   sum(network_bytes_avg * count::FLOAT8) / sum(count)::FLOAT8 AS net_bytes,",
			"   sum(run_lat_avg * count::FLOAT8) / sum(count)::FLOAT8 AS run_lat,",
			"   sum(rows_read_avg * count::FLOAT8) / sum(count)::FLOAT8 AS rows_read,",
			"   sum(rows_avg * count::FLOAT8) / sum(count)::FLOAT8 AS rows_avg,",
			"   sum(bytes_read_avg * count::FLOAT8) / sum(count)::FLOAT8 AS bytes_avg,",
			"   sum(run_lat_avg * count::FLOAT8) AS total_lat,",
			"   sum(max_retries) AS max_retries,",
			"   max(max_mem_usage_avg) AS max_mem,",
			"   sum(contention_time_avg * count::FLOAT8) / sum(count)::FLOAT8 AS cont_time",
			"FROM",
			"   crdb_internal.node_statement_statistics",
			"WHERE",
			"   application_name NOT LIKE '$ internal-%'",
			"GROUP BY",
			"   statement_id, application_name, database_name",
			"ORDER BY",
			"   total_lat DESC",
			"LIMIT",
			"   $1;"
		],
		"maxResults": 10,
		"frequency": 5,
		"labels": ["statement_id", "application_name", "database_name"],
		"metrics": [
		  {
			"type": "counter",
			"name": "crdb_exec_count",
			"desc": "exec count"
		  },
		  {
			"type": "gauge",
			"name": "crdb_max_disk",
			"desc": "crdb_max_disk"
		  },
		  {
			"type": "gauge",
			"name": "crdb_net_bytes",
			"desc": "crdb_net_bytes"
		  },
		  {
			"type": "gauge",
			"name": "crdb_run_lat",
			"desc": "crdb_run_lat"
		  },
		  {
			"type": "gauge",
			"name": "crdb_rows_read",
			"desc": "crdb_rows_read"
		  },
		  {
			"type": "gauge",
			"name": "crdb_rows_avg",
			"desc": "crdb_rows_avg"
		  },
		  {
			"type": "gauge",
			"name": "crdb_bytes_avg",
			"desc": "crdb_bytes_avg"
		  },
		  {
			"type": "gauge",
			"name": "crdb_total_lat",
			"desc": "crdb_total_lat"
		  },
		  {
			"type": "gauge",
			"name": "crdb_max_retries",
			"desc": "crdb_max_retries"
		  },
		  {
			"type": "gauge",
			"name": "crdb_max_mem",
			"desc": "crdb_max_mem"
		  },
		  {
			"type": "gauge",
			"name": "crdb_cont_time",
			"desc": "crdb_cont_time"
		  }
		]
	}
	`
	intentDef = `
	{
		"name": "intent",
		"query": [
		  "SELECT * FROM (",
		  "SELECT name, sum((crdb_internal.range_stats(start_key)->'intent_count')::int) as intent_count",
		  "FROM crdb_internal.ranges_no_leases AS r ",
		  "    JOIN crdb_internal.tables AS t ON r.table_id = t.table_id",
		  "    GROUP BY \"name\"",
		  ")",
		  "inline WHERE intent_count > 0 LIMIT $1;"
		],
		"maxResults": 10,
		"frequency": 10,
		"labels": [
		  "name"
		],
		"metrics": [
		  {
			"type": "gauge",
			"name": "crdb_intent_count",
			"desc": "number of intents per table"
		  }
		]
	  }
	`
)

func jsonCollector(pool database.PgxPool, name string, defJson string) Collector {
	var def CollectorConfig
	err := json.Unmarshal([]byte(defJson), &def)
	if err != nil {
		log.Fatalf("%s malformed", name)
	}
	coll := New(def.Name, def.Labels, strings.Join(def.Query, "\n"), pool)
	coll.WithMaxResults(int(def.MaxResults))
	coll.WithFrequency(int(def.Frequency))
	for _, m := range def.Metrics {
		switch m.Type {
		case "gauge":
			coll.AddGauge(m.Name, m.Desc)
		case "counter":
			coll.AddCounter(m.Name, m.Desc)
		default:
			log.Fatalf("%s malformed", name)
		}
	}
	return coll
}

func GetDefaultCollectors(pool database.PgxPool) []Collector {
	return []Collector{
		jsonCollector(pool, "intent", intentDef),
		jsonCollector(pool, "activity", activityDef),
	}
}
