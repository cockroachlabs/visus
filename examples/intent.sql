SELECT *
  FROM (
          SELECT name,
                 sum(
                    (crdb_internal.range_stats(start_key)->'intent_count')::INT8
                 ) AS intent_count
            FROM crdb_internal.ranges_no_leases AS r
            JOIN crdb_internal.tables AS t ON r.table_id = t.table_id
        GROUP BY name
       ) AS inline
 WHERE intent_count > 0
 LIMIT $1;