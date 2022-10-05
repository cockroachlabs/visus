CREATE ROLE IF NOT EXISTS visus_monitor;
ALTER ROLE visus_monitor WITH VIEWACTIVITY;

CREATE DATABASE IF NOT EXISTS _visus;

GRANT CONNECT ON DATABASE _visus to visus_monitor;

CREATE TYPE IF NOT EXISTS _visus.scope AS ENUM ('local', 'global');

CREATE TABLE IF NOT EXISTS _visus.collection (
    namespace     STRING DEFAULT '',
    name       STRING NOT NULL,
    updated    timestamptz DEFAULT current_timestamp (), 
    enabled    BOOL NOT NULL,
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
