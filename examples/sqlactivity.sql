SELECT
	statement_id as statement,
	application_name as application,
	database_name as database,
	sum(count) AS exec_count,
	max(max_disk_usage_avg) AS max_disk,
	sum(network_bytes_avg * count::FLOAT8) / sum(count)::FLOAT8 AS net_bytes,
	sum(run_lat_avg * count::FLOAT8) / sum(count)::FLOAT8 AS run_lat,
	sum(rows_read_avg * count::FLOAT8) / sum(count)::FLOAT8 AS rows_read,
	sum(rows_avg * count::FLOAT8) / sum(count)::FLOAT8 AS rows_avg,
	sum(bytes_read_avg * count::FLOAT8) / sum(count)::FLOAT8 AS bytes_avg,
	sum(run_lat_avg * count::FLOAT8) AS total_lat,
	sum(max_retries) AS max_retries,
	max(max_mem_usage_avg) AS max_mem,
	sum(contention_time_avg * count::FLOAT8) / sum(count)::FLOAT8 AS cont_time
FROM
	crdb_internal.node_statement_statistics
WHERE
	application_name NOT LIKE '$ internal-%'
GROUP BY
	statement_id, application_name, database_name
ORDER BY
	total_lat DESC
LIMIT
	$1;
