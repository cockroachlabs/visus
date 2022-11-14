UPSERT INTO _visus.histogram
   (name, regex, bins, "start", "end") 
VALUES 
   ($1, $2, $3, $4, $5)
