# visus
Visus (latin for "the action of looking") enables users to collect metrics using arbitrary SQL queries and expose them in a Prometheus format.
Visus runs as a sidecar on each node of a CockroachDB cluster;
it can also be used for any database that is compatible with the Postgres wire protocol.

Metrics are grouped in collections. Each collection uses a SQL query to collect the metrics, and determines how often the metrics need to be fetched.

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

Prometheus collectors will add additional labels to track the cluster name as well as the instance where the metrics are coming from.
## Example
For instance, to track the sql activity at a application and database level, we can use this yaml configuration (saved into the query_count.yaml file)
There are two labels (application, database) that are returned as the first 2 columns in the SQL query.
We have a metric, exec_count, which is a counter; its description is "statement count per application and database".
We would like to have at most 50 results and fetch the metrics every 10 seconds.

```yaml
name: query_count
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

Assuming we have a `$CRDB_URL` variable that defines the URL to connect to the database, for instance:

```bash
CRDB_URL="postgresql://root@localhost:26257/defaultdb?sslmode=disable"
```
Initialize the database that contains the configuration for the collections:

```bash
./visus collection init --url "$CRDB_URL" 
```

Then, we can create a new collection in the database using the `visus put` command. 

```bash
./visus collection put --url "$CRDB_URL" --yaml query_count.yaml 
```

Result:

```text
Collection query_count inserted.                            
```

List all the collection names in the database:

```bash
./visus collection list --url "$CRDB_URL"
```

Result:

```text
Collection: query_count
```

View the query_count collection definition:

```bash
./visus collection get query_count --url "$CRDB_URL"
```

Result:

```text
Collection: query_count
Labels:     application,database
Query:      SELECT
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

MaxResults: 10
Frequency:  10 seconds
Metrics:    [{exec_count counter statement count per application and database}]
```

Test the collection, and fetch the metrics:

```bash
./visus collection test query_count --url "$CRDB_URL"
```

Sample results:

```text
# HELP query_count_exec_count statement count per application and database
# TYPE query_count_exec_count counter
query_count_exec_count{application="",database="defaultdb"} 11
```

Start the server to enable collection of metrics from Prometheus.

```bash
./visus start --insecure --endpoint "/_status/custom"  --url "$CRDB_URL" 
```

## Collection management commands

Use the `visus collection` command to manage the collections in the database.

```text
Usage:
  visus collection [command]

Available Commands:
  delete
  test
  get
  init
  list
  put 

Flags:
  -h, --help         help for collection
      --url string   Connection URL, of the form: postgresql://[user[:passwd]@]host[:port]/[db][?parameters...]

Global Flags:
      --logDestination string   write logs to a file, instead of stdout
      --logFormat string        choose log output format [ fluent, text ] (default "text")
  -v, --verbose count           increase logging verbosity to debug; repeat for trace

Use "visus collection [command] --help" for more information about a command.
  ```

## Starting a server
To start the server, use the 'visus start' command:
```
Usage:
  visus start [flags]

Examples:

./visus start --bindAddr "127.0.0.1:15432" 

Flags:
      --bind-addr string   A network address and port to bind to (default "127.0.0.1:8888")
      --certs-dir string   Path to the directory containing TLS certificates and keys for the server
      --endpoint string    Endpoint for metrics. (default "/_status/vars")
  -h, --help               help for start
      --insecure           this flag must be set if no TLS configuration is provided
      --url string         Connection URL, of the form: postgresql://[user[:passwd]@]host[:port]/[db][?parameters...]

Global Flags:
      --logDestination string   write logs to a file, instead of stdout
      --logFormat string        choose log output format [ fluent, text ] (default "text")
  -v, --verbose count           increase logging verbosity to debug; repeat for trace
``` 
