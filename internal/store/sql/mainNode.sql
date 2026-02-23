-- Copyright 2024 The Cockroach Authors
--
-- Licensed under the Apache License, Version 2.0 (the "License");
-- you may not use this file except in compliance with the License.
-- You may obtain a copy of the License at
--
--     http://www.apache.org/licenses/LICENSE-2.0
--
-- Unless required by applicable law or agreed to in writing, software
-- distributed under the License is distributed on an "AS IS" BASIS,
-- WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
-- See the License for the specific language governing permissions and
-- limitations under the License.
--
-- SPDX-License-Identifier: Apache-2.0

-- Main node election uses max(id) among recently-active nodes. This provides
-- deterministic, stable leadership — the highest-ID active node always wins.
-- This is intentional: leadership is sticky and predictable, avoiding flapping.
--
-- Note: there is a brief window (bounded by the refresh interval) where a
-- previous main node may still run cluster-scoped collectors after leadership
-- changes. This is acceptable because duplicate metrics are idempotent.

WITH cleanup AS (
DELETE FROM
  _visus.node
WHERE
  updated < $1 - interval '24 hours'
RETURNING *
), touch as (
UPSERT INTO
  _visus.node
VALUES
  ((SELECT node_id::INT FROM [SHOW node_id]), current_timestamp())
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
  max(id) = (SELECT node_id::INT FROM [SHOW node_id])
FROM
  nodes;
