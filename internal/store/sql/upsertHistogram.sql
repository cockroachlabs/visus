
UPSERT INTO _visus.histogram  
   (regex, enabled, bins, "start", "end")
VALUES 
   ($1, $2, $3, $4, $5)
      