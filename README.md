# visus

Visus (latin for "the action of looking") enables users to collect metrics using arbitrary SQL queries and expose them in a Prometheus format. Optionally, it can be configured to extracts metrics from logs and filter the metrics CockroachDB available in the `/_status/vars` endpoint.

Visus runs as a sidecar on each node of a CockroachDB cluster, as shown in the diagram below.
It can also be used for any database that is compatible with the Postgres wire protocol.

!["Typical visus deployment"](visus.png)

## Metric configuration

Metrics are grouped in collections and stored in the `_visus` database. Each collection uses a SQL query to collect the metrics, and determines how often the metrics need to be fetched.

The SQL query result columns are mapped into labels and metrics values.
The labels define the dimensions for a metric (for instance the database name or the application name).
When configuring a collection, the administrator will specify the type of the metric (e.g. counter or gauge) and a description of metric.

The template of the query is as follows (common table expressions are allowed; a parameter for limit must be provided):

```sql
SELECT 
   label_1, ... ,label_n, metric_1, ... metric_m
FROM 
   ... 
WHERE 
   ... 
LIMIT 
   $1
```

The Prometheus collectors will add additional labels to track the cluster name as well as the instance where the metrics are coming from.

## Database Security

It is recommended to use separate users for managing the configuration and to run the sidecar.
The sidecar needs `SELECT ON TABLES` privileges on the `_visus` database to read the configuration. It also needs the `SELECT,INSERT,UPDATE` privileges on the `_visus.node` table, used to determine which node should collect cluster wide metrics.  

To run many of the sample collections available in the examples directory,
the 'VIEWACTIVITY' option should be granted to the user.
The `./visus init` command will provision a `visus` user with the minimal privileges to run the sidecar. Defining new collection may require additional privileges, depending on what data the SQL query associated to the collection has to access.

## Example

For instance, to track the sql activity at a application and database level, we can use this yaml configuration (saved into the query_count.yaml file)
There are two labels (application, database) that are returned as the first 2 columns in the SQL query.
We have a metric, exec_count, which is a counter; its description is "statement count per application and database".
We would like to have at most 50 results and fetch the metrics every 10 seconds.

```yaml
name: query_count
enabled: true
frequency: 10
maxresults: 50
labels: [application,database]
metrics:
  - name : exec_count
    kind : counter
    help : statement count per application and database.
query:
  SELECT
    application_name as application,
    database_name as database,
    sum(count) AS exec_count
  FROM
        crdb_internal.node_statement_statistics
  WHERE
        application_name NOT LIKE '$ internal-%'
  GROUP BY
        application_name, database_name
  ORDER BY
        exec_count DESC
  LIMIT
        $1;
```

We define the following environment variables to run the visus executable.

```bash
VISUS_PROM="http://localhost:8080/_status/vars"
VISUS_ADMIN="./visus --url postgresql://root@localhost:26257/defaultdb?sslmode=disable"
VISUS_USER="./visus --url postgresql://visus@localhost:26257/defaultdb?sslmode=disable"
```

Alternatively, to use the docker image, define:

```bash
VISUS_PROM="http://host.docker.internal:8080/_status/vars"
VISUS_ADMIN="docker run --rm -i cockroachdb/visus --url postgresql://root@host.docker.internal:26257/defaultdb?sslmode=disable"
VISUS_USER="docker run  -p 8888:8888 --rm -i cockroachdb/visus --url postgresql://visus@host.docker.internal:26257/defaultdb?sslmode=disable"
```

Initialize the database that contains the configuration for the collections:

```bash
$VISUS_ADMIN init
```

The init will create the tables and a `visus` user.

Now, we can create a new collection in the database using the `visus collection put` command.

```bash
$VISUS_ADMIN collection put --yaml - < query_count.yaml
```

Result:

```text
Collection query_count inserted.
```

List all the collection names in the database:

```bash
$VISUS_USER collection list 
```

Result:

```text
query_count
```

View the query_count collection definition:

```bash
$VISUS_USER collection get query_count 
```

Result:

```text
name: query_count
frequency: 10
maxresults: 100
enabled: true
query: SELECT application_name as application, database_name as database, sum(count) AS exec_count FROM crdb_internal.node_statement_statistics WHERE application_name NOT LIKE '$ internal-%' GROUP BY application_name, database_name ORDER BY exec_count DESC LIMIT $1;
labels:
    - application
    - database
metrics:
    - name: exec_count
      kind: counter
      help: statement count per application and database.
```

Test the collection, and fetch the metrics twice, with default interval (10 seconds):

```bash
$VISUS_USER collection test query_count --count 2
```

Sample results:

```text
---- 11-12-2022 17:03:09 query_count -----
# HELP query_count_exec_count statement count per application and database.
# TYPE query_count_exec_count counter
query_count_exec_count{application="",database="_visus"} 7
query_count_exec_count{application="",database="defaultdb"} 16

---- 11-12-2022 17:03:19 query_count -----
# HELP query_count_exec_count statement count per application and database.
# TYPE query_count_exec_count counter
query_count_exec_count{application="",database="_visus"} 7
query_count_exec_count{application="",database="defaultdb"} 17
```

Start the server to enable collection of metrics from Prometheus.

```bash
$VISUS_USER start --bind-addr :8888 --insecure --endpoint "/_status/custom" 
```

## Scanning logs

Visus can also be used to scan cockroachdb log files: it will produce metrics
for the events in the logs, adding labels for the level and the source of the
event. For instance to collect all the events in the cockroack.log file, define
a scan configuration `log.yaml` as follows:

```yaml
name: cockroach_log
enabled: true
format: crdb-v2
path: /var/log/cockroach.log
patterns:
  - name : events
    regex:
    help : number of events
```

Insert the scan configuration in the database:

```bash
$VISUS_ADMIN  scan put --yaml - < log.yaml
```

Example of the metrics produced:

```text
# HELP cockroach_log_events number of events
# TYPE cockroach_log_events counter
cockroach_log_events{level="I",source="cli/log_flags.go"} 6
cockroach_log_events{level="I",source="cli/start.go"} 102
cockroach_log_events{level="I",source="gossip/client.go"} 9
cockroach_log_events{level="I",source="gossip/gossip.go"} 6
cockroach_log_events{level="I",source="jobs/job_scheduler.go"} 3
cockroach_log_events{level="I",source="jobs/registry.go"} 12
```

## Scanning auth logs

Visus can also be used to scan cockroachdb sql authorization log files: it will produce metrics
for each successful authentication event.
For instance to collect all the authentication events in the cockroach-sql-auth.log file, define
a scan configuration `auth.yaml` as follows:

```yaml
name: cockroach_auth_log
enabled: true
format: crdb-v2-auth
path: /var/log/cockroach-sql-auth.log
patterns:
  - name : auth
    help : number of successful login per user
```

Insert the scan configuration in the database:

```bash
$VISUS_ADMIN  scan put --yaml - < auth.yaml
```

Example of the metrics produced:

```text
# HELP crdb_auth number of successful login per user
# TYPE crdb_auth counter
crdb_auth{event="client_authentication_ok",indentity="craig",method="cert-password",transport="hostssl",user="craig"} 1
crdb_auth{event="client_authentication_ok",indentity="roachprod",method="cert-password",transport="hostssl",user="roachprod"} 2
crdb_auth{event="client_authentication_ok",indentity="root",method="cert-password",transport="hostssl",user="root"} 3
```

## Histogram rewriting

Visus can also act as a proxy to filter and rewrite CockroachDB histograms (v22.1 and earlier) from a log-2 linear format (HDR histograms) to a log-10 linear format.
Users can specify which histograms to rewrite based on a regular expression. For instance to rewrite all the histograms that match "^sql_exec_latency$" and keep buckets between 1ms and 20sec, we specify in the configuration file `latency.yaml`:

```yaml
name: latency
regex: ^sql_exec_latency$
enabled: true
start: 1000000
end: 20000000000
```

```bash
$VISUS_ADMIN  histogram put --yaml - < latency.yaml
```

Result:

```text
histogram latency inserted.
```

To enable filter in the server, start the server to enable collection of metrics from Prometheus, specify the collection endpoint with the `--promethues` flag.

```bash
$VISUS_USER start --bind-addr :8888 --rewrite-histograms --insecure --endpoint "/_status/custom" --prometheus $VISUS_PROM
```

## Commands

### Database management commands

Use the `visus init` command to initialize database.

```text
Usage:
  visus init [flags]

Examples:
./visus init  --url "postgresql://root@localhost:26257/defaultdb?sslmode=disable" 

Flags:
  -h, --help         help for init
      --url string   Connection URL, of the form: postgresql://[user[:passwd]@]host[:port]/[db][?parameters...]

Global Flags:
      --logDestination string   write logs to a file, instead of stdout
      --logFormat string        choose log output format [ fluent, text ] (default "text")
  -v, --verbose count           increase logging verbosity to debug; repeat for trace
```

### Collection management commands

Use the `visus collection` command to manage the collections in the database.

```text
Usage:
  visus collection [command]

Available Commands:
  delete      
  get         
  list        
  put         
  test        

Flags:
  -h, --help         help for collection
      --url string   Connection URL, of the form: postgresql://[user[:passwd]@]host[:port]/[db][?parameters...]

Global Flags:
      --logDestination string   write logs to a file, instead of stdout
      --logFormat string        choose log output format [ fluent, text ] (default "text")
  -v, --verbose count           increase logging verbosity to debug; repeat for trace

Use "visus collection [command] --help" for more information about a command.
```

### Scan management commands

Use the `visus scan` command to manage the log scans in the database.

```text
Usage:
  visus scan [command]

Available Commands:
  delete
  get
  list
  put
  test

Flags:
  -h, --help         help for scan
      --url string   Connection URL, of the form: postgresql://[user[:passwd]@]host[:port]/[db][?parameters...]

Global Flags:
      --logDestination string   write logs to a file, instead of stdout
      --logFormat string        choose log output format [ fluent, text ] (default "text")
  -v, --verbose count           increase logging verbosity to debug; repeat for trace

Use "visus scan [command] --help" for more information about a command.
```

### Histogram filter management commands

Use the `visus histogram` command to manage the collections in the database.

```text
Usage:
  visus histogram [command]

Available Commands:
  delete
  get
  list
  put
  test

Flags:
  -h, --help         help for histogram
      --url string   Connection URL, of the form: postgresql://[user[:passwd]@]host[:port]/[db][?parameters...]

Global Flags:
      --logDestination string   write logs to a file, instead of stdout
      --logFormat string        choose log output format [ fluent, text ] (default "text")
  -v, --verbose count           increase logging verbosity to debug; repeat for trace

Use "visus histogram [command] --help" for more information about a command.
```

### Starting a server

To start the server, use the `visus start` command:

```text
Usage:
  visus start [flags]

Examples:

./visus start --bindAddr "127.0.0.1:15432" 

Flags:
      --bind-addr string     A network address and port to bind to (default "127.0.0.1:8888")
      --bind-cert string     Path to the  TLS certificate for the server
      --bind-key string      Path to the  TLS key for the server
      --ca-cert string       Path to the  CA certificate
      --endpoint string      Endpoint for metrics. (default "/_status/vars")
  -h, --help                 help for start
      --insecure             this flag must be set if no TLS configuration is provided
      --proc-metrics         enable the collection of process metrics
      --prometheus string    prometheus endpoint
      --refresh duration     How ofter to refresh the configuration from the database. (default 5m0s)
      --rewrite-histograms   enable histogram rewriting
      --url string           Connection URL, of the form: postgresql://[user[:passwd]@]host[:port]/[db][?parameters...]
      --visus-metrics        enable the collection of visus metrics

Global Flags:
      --logDestination string   write logs to a file, instead of stdout
      --logFormat string        choose log output format [ fluent, text ] (default "text")
  -v, --verbose count           increase logging verbosity to debug; repeat for trace
```
