-- Copyright 2026 The Cockroach Authors
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

-- Only return nodes whose heartbeat is within the last 5 minutes.
-- This window should be at least 2x the heartbeat interval to avoid
-- falsely marking healthy nodes as stale.
SELECT
	id, hostname, pid, version, updated
FROM
	_visus.node
WHERE
	updated > (current_timestamp() - INTERVAL '5 minutes')
ORDER BY
	updated DESC
