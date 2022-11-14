UPSERT INTO _visus.collection
   (name, enabled, scope, maxResults, frequency, query, labels, updated) 
VALUES 
   ($1,$2, $3, $4, $5, $6, $7, current_timestamp())
