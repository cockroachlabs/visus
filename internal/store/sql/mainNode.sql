WITH touch as (
UPSERT INTO
  _visus.node 
VALUES 
  (crdb_internal.node_id(), current_timestamp())
RETURNING _visus.node.*
), nodes as (
SELECT
  *
FROM
  _visus.node 
WHERE 
  updated >= $1
UNION
SELECT
  *
FROM
  touch
)
SELECT 
  max(id) = crdb_internal.node_id()
FROM 
  nodes;
