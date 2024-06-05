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

CREATE DATABASE IF NOT EXISTS _visus;

CREATE USER IF NOT EXISTS visus;
ALTER ROLE visus WITH VIEWACTIVITY;
GRANT CONNECT ON DATABASE _visus to visus;

USE _visus;

ALTER DEFAULT PRIVILEGES GRANT SELECT ON TABLES TO visus;

CREATE TYPE IF NOT EXISTS _visus.scope AS ENUM ('node', 'cluster');

CREATE TABLE IF NOT EXISTS _visus.collection (
    namespace     STRING DEFAULT '',
    name       STRING NOT NULL,
    updated    timestamptz DEFAULT current_timestamp (),
    enabled    BOOL DEFAULT true,
    scope      _visus.scope NOT NULL,
    maxResults INT NOT NULL,
    frequency  INTERVAL NOT NULL,
    query      STRING NOT NULL,
    labels     STRING[],
    PRIMARY KEY (namespace, name)
);

CREATE TYPE IF NOT EXISTS _visus.kind AS ENUM ('gauge', 'counter');

CREATE TABLE IF NOT EXISTS _visus.metric (
    collection   STRING NOT NULL,
    metric       STRING NOT NULL,
    kind        _visus.kind  NOT NULL,
    help        STRING NOT NULL,
    PRIMARY KEY (collection, metric)
);


CREATE TABLE IF NOT EXISTS _visus.histogram (
    name         STRING NOT NULL,
    regex        STRING NOT NULL,
    updated      timestamptz DEFAULT current_timestamp (),
    bins         INT NOT NULL,
    "start"      INT NOT NULL,
    "end"        INT NOT NULL,
    enabled      BOOL DEFAULT true,
    PRIMARY KEY (name)
);

CREATE TABLE IF NOT EXISTS _visus.node (
    id           INT PRIMARY KEY,
    updated      timestamptz DEFAULT current_timestamp ()
);

GRANT SELECT,INSERT,UPDATE ON TABLE _visus.node TO visus;
GRANT SELECT ON TABLE _visus.collection TO visus;
GRANT SELECT ON TABLE _visus.metric TO visus;
GRANT SELECT ON TABLE _visus.histogram TO visus;


CREATE TABLE IF NOT EXISTS _visus.scan (
    name         STRING NOT NULL,
    path         STRING NOT NULL,
    format       STRING NOT NULL,
    updated      timestamptz DEFAULT current_timestamp (),
    enabled      BOOL DEFAULT true,
    PRIMARY KEY (name)
);

CREATE TABLE IF NOT EXISTS _visus.pattern (
    scan         STRING NOT NULL,
    metric       STRING NOT NULL,
    help         STRING NOT NULL,
    regex        STRING NOT NULL,
    PRIMARY KEY (scan, metric)
);

GRANT SELECT ON TABLE _visus.scan TO visus;
GRANT SELECT ON TABLE _visus.pattern TO visus;
