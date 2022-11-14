select name, regex, updated, "enabled", bins, "start", "end" from _visus.histogram where name = $1;
