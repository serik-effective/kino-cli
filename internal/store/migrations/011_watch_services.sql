-- Where a film can actually be watched, as a JSON list of service names.
--
-- watch_online answered only "is it on Kinopoisk", because that was all the
-- player payload told us. The catalogue API reports every service — Кинопоиск,
-- Иви, КИОН, START and the rest — which is the difference between "streaming
-- somewhere" and "streaming where you have a subscription".
ALTER TABLE movies ADD COLUMN watch_services TEXT;
