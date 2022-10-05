
UPSERT INTO _visus.histogram  
   (regex, bins, "start", "end") 
VALUES 
   ($1, $2, $3, $4)
      