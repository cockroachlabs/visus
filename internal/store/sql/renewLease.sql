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

-- Lease-based leader election using the single-row _visus.leader table.
--
-- Try to renew this node's lease and return the current leader state.
-- Returns (renewed, lease_valid):
--   (true,  true)  – this node renewed its lease successfully.
--   (false, true)  – another node holds a valid lease; skip claim.
--   (false, false) – lease expired; caller should attempt to claim.

WITH renew AS (
    UPDATE _visus.leader
    SET lease_expires_at = current_timestamp() + $1::INTERVAL
    WHERE node_id = $2
      AND nonce = $3
      AND lease_expires_at > current_timestamp()
    RETURNING true AS renewed
)
SELECT COALESCE(r.renewed, false) AS renewed,
       l.lease_expires_at > current_timestamp() AS lease_valid
FROM _visus.leader l
LEFT JOIN renew r ON true;
