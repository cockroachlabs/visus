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

-- Claim leadership if the lease has expired. First writer wins.
-- Returns true if claim succeeded, false otherwise.

UPDATE _visus.leader
SET node_id = $2,
    lease_expires_at = current_timestamp() + $1::INTERVAL,
    nonce = gen_random_uuid()
WHERE lease_expires_at <= current_timestamp()
RETURNING nonce;
