UPSERT INTO _visus.scan
   (name, path, format, enabled, updated)
VALUES
   ($1, $2, $3, $4, current_timestamp())
