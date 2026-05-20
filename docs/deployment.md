# Deploying and administering visus

This document covers how to install, configure, and operate visus as a
metrics sidecar. See the project [README](../README.md) for the
SQL-to-Prometheus concepts the rest of this doc assumes.

## Overview

Visus runs as a per-node sidecar next to a CockroachDB (or
Postgres-compatible) instance. It exposes a Prometheus scrape endpoint
that publishes two kinds of metrics: results of scheduled SQL
**collections**, and counters produced by log-file **scanners**. Both are
defined in YAML, stored in a `_visus` configuration database, and refreshed
into the running process on a timer or on `SIGHUP`.

Visus is intended to publish **additional** metrics that CockroachDB does
not expose directly (custom SQL aggregations, log-derived counters, and
so on). The built-in CockroachDB metrics should still be scraped from
the CockroachDB node itself (`/_status/vars`) by Prometheus. Configure
two scrape jobs: one pointed at CockroachDB, one pointed at the visus
sidecar.

## Prerequisites

- A CockroachDB cluster (or any Postgres-wire-compatible database)
  reachable from the sidecar host.
- Two SQL roles:
  - an **admin role** (typically `root`) used by `visus init` and by
    `visus collection put` / `visus scan put`,
  - the **`visus`** role created by `visus init`, used by the running
    sidecar for reads.
- A Prometheus instance with network access to the sidecar's bind address.

## Install

### Binary

```bash
go install github.com/cockroachlabs/visus@latest
# or download a release artifact and place it at /usr/local/bin/visus
```

### Docker

```bash
docker pull cockroachdb/visus
```

The image is built from the repository `Dockerfile` and ships a scratch
base, so all paths inside the container are absolute.

## Bootstrap the configuration database

Run `visus init` once per cluster, with admin credentials:

```bash
visus init \
  --url "postgresql://root@localhost:26257/defaultdb?sslmode=disable"
```

This creates the `_visus` database and its tables (`collection`, `metric`,
`scan`, `pattern`, `node`, `lease`), creates the `visus` SQL user with
`VIEWACTIVITY` and the necessary SELECT grants, grants
`SELECT,INSERT,UPDATE,DELETE` on `_visus.node` for heartbeat
registration, and grants `SELECT,INSERT,UPDATE` on `_visus.lease` for
cluster-scope coordination.

If a collection's query reads tables beyond `crdb_internal` /
`_visus`, the `visus` role needs SELECT on those tables as well. Grant
them separately.

## Defining collections

A collection is a SQL query that visus runs on a fixed interval. The
query's named output columns become Prometheus label values (one series
per result row) and metric values (one series per declared metric). The
query must end with `LIMIT $1`; visus binds `maxresults` to that
parameter.

Fields supported by the YAML schema:

| Field        | Type           | Notes                                                   |
| ------------ | -------------- | ------------------------------------------------------- |
| `name`       | string         | Unique per database. Becomes the metric name prefix.    |
| `enabled`    | bool           | Set `false` to retain the config without scraping.      |
| `scope`      | `node`/`cluster` | `node` runs on every sidecar; `cluster` runs on one.  |
| `frequency`  | int (seconds)  | Scrape interval.                                        |
| `maxresults` | int            | Bound for `LIMIT $1` in the query.                      |
| `labels`     | list of strings | Output columns to use as Prometheus labels.            |
| `metrics`    | list           | Each has `name`, `kind` (`counter`/`gauge`), `help`.    |
| `databases`  | string (optional SQL) | Per-database iteration; see below.               |
| `query`      | string         | The SQL. Must end with `LIMIT $1`.                      |

A canonical example lives at
[`examples/sqlactivity.yaml`](../examples/sqlactivity.yaml). Manage
collections with the `visus collection` subcommands:

```bash
visus collection put --url "$ADMIN_URL" --yaml - < examples/sqlactivity.yaml
visus collection list --url "$URL"
visus collection get  --url "$URL" sqlactivity
visus collection test --url "$URL" sqlactivity --count 2
visus collection delete --url "$ADMIN_URL" sqlactivity
```

### Cluster-scoped collections

When `scope: cluster`, only one sidecar runs the collection at a time;
the others stay idle for that one. Election uses a row in `_visus.lease`
held by the active sidecar (identified by `hostname:pid`) and refreshed
before it expires. Use cluster scope for queries that already produce
cluster-wide results (`SHOW DATABASES`, `crdb_internal.cluster_*`) to
avoid duplicate series.

### Per-database queries

For queries that are inherently database-scoped (`SHOW TABLES`,
`crdb_internal` tables that are scoped to the current database), provide
a `databases` SQL snippet that returns the database names to iterate
over. Visus runs the main `query` once per returned name and adds a
`_database` label to the result. See `examples/tables_rows.yaml` for the
pattern.

## Defining scanners

A scanner tails a CockroachDB log file in `crdb-v2` or `crdb-v2-auth`
format and increments a Prometheus counter whenever a line matches one
of its regex patterns. Scanners do not touch the database during
execution; their config is the only thing stored in `_visus`.

Fields:

| Field      | Type     | Notes                                              |
| ---------- | -------- | -------------------------------------------------- |
| `name`     | string   | Unique. Becomes the metric name prefix.            |
| `enabled`  | bool     | Set `false` to retain the config without tailing.  |
| `format`   | string   | `crdb-v2` or `crdb-v2-auth`.                       |
| `path`     | string   | Absolute path to the log file.                     |
| `patterns` | list     | Each has `name`, `regex`, `help`, optional `exclude`. |

Minimal example for counting `jobs` log lines while excluding stack
traces:

```yaml
name: cockroach_log
enabled: true
format: crdb-v2
path: /var/log/cockroach/cockroach.log
patterns:
  - name: jobs
    regex: jobs
    help: number of job events
    exclude: \] \d+ \+
```

CLI management mirrors collections:

```bash
visus scan put    --url "$ADMIN_URL" --yaml - < cockroach_log.yaml
visus scan list   --url "$URL"
visus scan get    --url "$URL" cockroach_log
visus scan test   --url "$URL" cockroach_log --count 5
visus scan delete --url "$ADMIN_URL" cockroach_log
```

Pass `--inotify` to `visus start` to use filesystem notifications
instead of polling.

## Running the sidecar

```bash
visus start \
  --url "postgresql://visus@localhost:26257/defaultdb?sslmode=disable" \
  --bind-addr ":8888" \
  --endpoint "/_status/custom" \
  --refresh 5m \
  --insecure
```

Notable flags (see `visus start --help` for the full list):

- `--bind-addr` (default `127.0.0.1:8888`): listen address.
- `--endpoint` (default `/_status/vars`): scrape path.
- `--refresh` (default `5m`): how often to reload config from `_visus`.
- `--insecure`: required when no TLS material is provided.
- `--bind-cert` / `--bind-key` / `--ca-cert`: TLS for the scrape endpoint.
- `--inotify`: filesystem notifications for scanners.
- `--proc-metrics`, `--visus-metrics`: emit Go process and visus-internal
  metrics on the same endpoint.
- `--allow-unsafe-internals`: needed in CockroachDB v26+ for queries
  that touch `crdb_internal` over the read-only connection.

## Deployment patterns

### Bare binary

```bash
ADMIN_URL="postgresql://root@localhost:26257/defaultdb?sslmode=disable"
URL="postgresql://visus@localhost:26257/defaultdb?sslmode=disable"

visus init               --url "$ADMIN_URL"
visus collection put     --url "$ADMIN_URL" --yaml - < examples/sqlactivity.yaml
visus start              --url "$URL" --bind-addr :8888 --insecure
```

### Docker container

```bash
docker run --rm -p 8888:8888 \
  -v /var/log/cockroach:/var/log/cockroach:ro \
  cockroachdb/visus start \
    --url "postgresql://visus@host.docker.internal:26257/defaultdb?sslmode=disable" \
    --bind-addr ":8888" \
    --insecure
```

The read-only log mount is only needed when scanners are configured.
Drop it for collection-only deployments.

### systemd

`/etc/systemd/system/visus.service`:

```ini
[Unit]
Description=visus metrics sidecar
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=visus
Group=visus
ExecStart=/usr/local/bin/visus start \
  --url ${VISUS_URL} \
  --bind-addr :8888 \
  --endpoint /_status/custom \
  --insecure
ExecReload=/bin/kill -HUP $MAINPID
Restart=on-failure
RestartSec=5s
EnvironmentFile=/etc/visus/visus.env

[Install]
WantedBy=multi-user.target
```

Put the connection URL in `/etc/visus/visus.env` (`VISUS_URL=...`) so
secrets stay out of the unit file. The `visus` OS user must have read
access to the CockroachDB log directory if scanners are configured.
`systemctl reload visus` then triggers an immediate config refresh
without restarting the process.

### Kubernetes sidecar

Add visus as a second container in the CockroachDB Pod (or your
StatefulSet template), sharing the log volume:

```yaml
spec:
  containers:
    - name: cockroachdb
      image: cockroachdb/cockroach:latest
      volumeMounts:
        - name: logs
          mountPath: /cockroach/cockroach-data/logs
    - name: visus
      image: cockroachdb/visus
      args:
        - start
        - --url=postgresql://visus@localhost:26257/defaultdb?sslmode=disable
        - --bind-addr=:8888
        - --endpoint=/_status/custom
        - --insecure
      ports:
        - name: metrics
          containerPort: 8888
      volumeMounts:
        - name: logs
          mountPath: /cockroach/cockroach-data/logs
          readOnly: true
  volumes:
    - name: logs
      emptyDir: {}
```

Annotate the Pod (or use a `PodMonitor`) so Prometheus scrapes the
sidecar:

```yaml
metadata:
  annotations:
    prometheus.io/scrape: "true"
    prometheus.io/port: "8888"
    prometheus.io/path: "/_status/custom"
```

## Day-to-day administration

- **Edit config**: `visus collection put` / `visus scan put` writes to
  `_visus`. The sidecar reads it on the next `--refresh` tick (default
  5m). To pick it up immediately, send `SIGHUP`
  (`kill -HUP $(pidof visus)` or `systemctl reload visus`).
- **Disable without deleting**: set `enabled: false` and `put` the
  config again. Preferred over `delete` when you expect to turn it back
  on.
- **List live sidecars**: `visus node list`. Heartbeats are written
  every minute; the CLI filters to the last 5 minutes.
- **Verify output**: `curl http://host:8888/_status/custom` (or whatever
  `--endpoint` you chose).

## Troubleshooting

### Metric series disappear after editing a collection or scanner

The Prometheus Go client treats a metric's name plus its label set and
help string as immutable for the lifetime of the registry (see
[`prometheus.Register`](https://pkg.go.dev/github.com/prometheus/client_golang/prometheus#Register)).
Changing any of these on an already-registered config means the new
descriptor cannot be swapped in live:

- the `labels` list on a collection,
- the `metrics[i].name` or `metrics[i].help` on a collection,
- the `patterns[i].name` or `patterns[i].help` on a scanner.

When this happens, the sidecar log will contain a line like

```
ERROR Error scheduling collector <name>: a previously registered descriptor with the same fully-qualified name as Desc{...} has different label names or a different help string
```

(or `Error adding scanner <name>: ...` for a scanner). The old series
stops updating because the new collector failed to register.

Restart the sidecar. The new descriptor registers cleanly on a fresh
registry. Counters reset to zero; gauges are repopulated on the next
collection cycle. If the deployment runs multiple visus processes
(for example, one sidecar per CockroachDB node), restart all of them
so every instance picks up the updated descriptor.

### Cluster-scoped collection looks inactive

Only the lease holder runs it. `visus node list` shows registered
sidecars; check each one's log for messages like
`Skipping ... scope: cluster; holder: false` versus `holder: true`.

### `crdb_internal` queries fail on CockroachDB v26+

Add `--allow-unsafe-internals` to both `visus start` and
`visus collection test`. The flag sets `allow_unsafe_internals = true`
on the read-only connection.

### Scanner produces no metrics

Verify the `path` is correct and readable by the user running the
sidecar. Confirm the file is in `crdb-v2` or `crdb-v2-auth` format. Run
`visus scan test <name>` against the same file to see whether patterns
match. Without `--inotify`, the scanner polls, so give it a few seconds
after a config change.

### Read-only connection rejects a query the admin user can run

The `visus` role only has the SELECT privileges granted by `visus init`
and runs with `default_transaction_use_follower_reads = 'true'`. Grant
SELECT on whatever tables the new collection touches.

## Inspecting `_visus` directly

When the CLI is unreachable, or you want to inspect or diff multiple
rows at once, query the configuration tables directly.

Confirm `init` ran and the schema is present:

```sql
SHOW TABLES FROM _visus;
```

Expect at least `collection`, `metric`, `scan`, `pattern`, `node`,
`lease`.

List collections with their state:

```sql
SELECT name, enabled, scope, frequency, maxresults, labels, updated
FROM _visus.collection
ORDER BY name;
```

See the metrics declared by a collection:

```sql
SELECT metric, kind, help
FROM _visus.metric
WHERE collection = 'sqlactivity';
```

List scanners and their patterns:

```sql
SELECT name, enabled, format, path, updated FROM _visus.scan;

SELECT scan, metric, regex, exclude, help
FROM _visus.pattern
WHERE scan = 'cockroach_log';
```

See which sidecars are currently registered:

```sql
SELECT id, hostname, pid, version, updated
FROM _visus.node
ORDER BY updated DESC;
```

Rows are heartbeat-driven and TTL-cleaned at 2 h. `visus node list`
filters to the last ~5 minutes, so this table may contain rows older
than what the CLI shows.

See who currently holds each cluster-scope lease:

```sql
SELECT name, holder, expires
FROM _visus.lease
ORDER BY name;
```

`holder` is `hostname:pid` of the sidecar that won the election. A row
whose `expires` is in the past means no sidecar is currently renewing
it; the next acquisition attempt will take it over.

Treat these queries as read-only. Editing rows directly bypasses the
CLI's validation and reload signaling; use `collection put` or
`scan put` for changes.
