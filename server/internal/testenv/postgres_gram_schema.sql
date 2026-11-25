-- Deployed databases keep application tables in the gram schema and connect
-- with search_path=gram. Test databases do the same, so code that assumes the
-- public schema fails in tests instead of in production.
CREATE SCHEMA gram;
ALTER DATABASE gotestdb SET search_path = gram;
