-- Marker migration. The app startup guard creates/backfills pihak_1 and pihak_2
-- only when they are missing, which is safer across existing databases.
SELECT 1;
