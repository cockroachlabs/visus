CREATE ROLE IF NOT EXISTS visus;
ALTER USER visus WITH VIEWACTIVITY;

CREATE DATABASE IF NOT EXISTS _visus;

GRANT CONNECT ON DATABASE _visus to visus;

USE _visus;

ALTER DEFAULT PRIVILEGES GRANT SELECT ON TABLES TO visus_monitor;

CREATE TYPE IF NOT EXISTS _visus.scope AS ENUM ('local', 'global');

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
    regex        STRING NOT NULL,
    updated    timestamptz DEFAULT current_timestamp (), 
    bins         INT NOT NULL,
    "start"      INT NOT NULL,
    "end"        INT NOT NULL,
    enabled      BOOL DEFAULT true,
    PRIMARY KEY (regex)
);