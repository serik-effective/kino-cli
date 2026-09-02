ALTER TABLE movies ADD COLUMN release_date TEXT;
CREATE INDEX idx_movies_release_date ON movies(release_date);
