ALTER TABLE movies ADD COLUMN ratings_at TEXT;
CREATE INDEX idx_movies_ratings_at ON movies(ratings_at);
