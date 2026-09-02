-- When we first saw the film available in the Kinopoisk player.
--
-- For Russian cinema this stands in for the digital release date: TMDB records
-- no Digital release for most of it (48 of the 81 films we know to be streaming
-- in Russia have no release of type 4 at all), so the player is the only signal
-- that a film can be watched at home. The date is only as precise as our first
-- capture of the payload, which is why it is stored separately from ratings_at
-- and never overwritten once set.
ALTER TABLE movies ADD COLUMN watch_seen_at TEXT;
