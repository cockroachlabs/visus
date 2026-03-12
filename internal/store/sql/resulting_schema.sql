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

CREATE TABLE public.collection (
    namespace STRING NOT NULL DEFAULT '':::STRING,
    name STRING NOT NULL,
    updated TIMESTAMPTZ NULL DEFAULT current_timestamp():::TIMESTAMPTZ,
    enabled BOOL NULL DEFAULT true,
    scope public.scope NOT NULL,
    maxresults INT8 NOT NULL,
    frequency INTERVAL NOT NULL,
    query STRING NOT NULL,
    labels STRING[] NULL,
    databases STRING NULL DEFAULT '':::STRING,
    CONSTRAINT collection_pkey PRIMARY KEY (namespace ASC, name ASC)
) WITH (schema_locked = true);

CREATE TABLE public.metric (
    collection STRING NOT NULL,
    metric STRING NOT NULL,
    kind public.kind NOT NULL,
    help STRING NOT NULL,
    CONSTRAINT metric_pkey PRIMARY KEY (collection ASC, metric ASC)
) WITH (schema_locked = true);

CREATE TABLE public.histogram (
    name STRING NOT NULL,
    regex STRING NOT NULL,
    updated TIMESTAMPTZ NULL DEFAULT current_timestamp():::TIMESTAMPTZ,
    bins INT8 NOT NULL,
    start INT8 NOT NULL,
    "end" INT8 NOT NULL,
    enabled BOOL NULL DEFAULT true,
    CONSTRAINT histogram_pkey PRIMARY KEY (name ASC)
) WITH (schema_locked = true);

CREATE TABLE public.node (
    id INT8 NOT NULL DEFAULT unique_rowid(),
    updated TIMESTAMPTZ NULL DEFAULT current_timestamp():::TIMESTAMPTZ,
    hostname STRING NOT NULL DEFAULT '':::STRING,
    pid INT8 NOT NULL DEFAULT 0:::INT8,
    version STRING NOT NULL DEFAULT '':::STRING,
    CONSTRAINT node_pkey PRIMARY KEY (id ASC)
) WITH (
    ttl = 'on',
    ttl_expiration_expression = e'((updated) + INTERVAL \'2 hours\')',
    ttl_job_cron = '@daily',
    schema_locked = true
);

CREATE TABLE public.scan (
    name STRING NOT NULL,
    path STRING NOT NULL,
    format STRING NOT NULL,
    updated TIMESTAMPTZ NULL DEFAULT current_timestamp():::TIMESTAMPTZ,
    enabled BOOL NULL DEFAULT true,
    CONSTRAINT scan_pkey PRIMARY KEY (name ASC)
) WITH (schema_locked = true);

CREATE TABLE public.pattern (
    scan STRING NOT NULL,
    metric STRING NOT NULL,
    help STRING NOT NULL,
    regex STRING NOT NULL,
    exclude STRING NULL DEFAULT '':::STRING,
    CONSTRAINT pattern_pkey PRIMARY KEY (scan ASC, metric ASC)
) WITH (schema_locked = true);

CREATE TABLE public.lease (
    name STRING NOT NULL,
    expires TIMESTAMP NOT NULL,
    nonce UUID NOT NULL,
    holder STRING NOT NULL,
    CONSTRAINT lease_pkey PRIMARY KEY (name ASC)
) WITH (schema_locked = true);
