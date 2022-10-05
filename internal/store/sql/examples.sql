INSERT INTO collection 
VALUES ('intents', current_timestamp():::TIMESTAMP, true, 'global', 10, '10 seconds', 
        'SELECT * FROM (SELECT name, sum((crdb_internal.range_stats(start_key)->''intent_count'')::int) as intent_count  FROM crdb_internal.ranges_no_leases AS r  JOIN crdb_internal.tables AS t ON r.table_id = t.table_id GROUP BY name)  inline WHERE intent_count > 0 LIMIT $1',
         ARRAY['name']);


INSERT INTO metric   
VALUES ('intents','intent_count', 'gauge', 'intent count');


INSERT INTO collection 
VALUES ('sql_activity', current_timestamp():::TIMESTAMP, true, 'node', 10, '10 seconds', 
        '
SELECT
	statement_id as label_statement,
	application_name,
	database_name,
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
	application_name NOT LIKE ''$ internal-%''
GROUP BY
	statement_id, application_name, database_name
ORDER BY
	total_lat DESC
LIMIT
	$1;',
         ARRAY['statement','application','database']);

INSERT INTO metric  VALUES ('sql_activity', 'exec_count', 'counter', 'exec_count');

INSERT INTO metric  VALUES ('sql_activity','max_disk', 'gauge', 'max_disk');

INSERT INTO metric  VALUES ('sql_activity','net_bytes',  'gauge', 'net_bytes');

INSERT INTO metric  VALUES ('sql_activity','run_lat', 'gauge', 'run_lat');

INSERT INTO metric  VALUES ('sql_activity','rows_read',  'gauge', 'rows_read');

INSERT INTO metric  VALUES ('sql_activity','rows_avg', 'gauge', 'rows_avg');

INSERT INTO metric  VALUES ('sql_activity','bytes_avg',  'gauge', 'bytes_avg');

INSERT INTO metric  VALUES ('sql_activity','total_lat',  'gauge', 'total_lat');

INSERT INTO metric  VALUES ('sql_activity','max_retries',  'gauge', 'max_retries');

INSERT INTO metric  VALUES ('sql_activity','max_mem',  'gauge', 'max_mem');

INSERT INTO metric  VALUES ('sql_activity','cont_time',  'gauge', 'cont_time');