-- Tables used by the statechart Postgres driver.

-- One row per machine instance, holding its latest snapshot.
CREATE TABLE IF NOT EXISTS statechart_instances (
  machine TEXT NOT NULL,
  id TEXT NOT NULL,
  seq BIGINT NOT NULL,
  snapshot JSONB NOT NULL,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (machine, id)
);

-- The outbox: one row per effect that still has to be carried out. A row is
-- deleted once its effect has run, or when the machine cancels it. effect_id
-- is the timer or service run ID and is empty for actions.
CREATE TABLE IF NOT EXISTS statechart_effects (
  id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  machine TEXT NOT NULL,
  instance_id TEXT NOT NULL,
  kind TEXT NOT NULL,
  effect_id TEXT NOT NULL,
  effect JSONB NOT NULL,
  run_after timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

-- Finds the next due effect for a machine. Workers claim due effects in id
-- order, so a deployment that lets a large backlog of due effects build up
-- may want an index on (machine, id) as well; one with many pending timers
-- and a short backlog is better served by this one.
CREATE INDEX IF NOT EXISTS statechart_effects_due_idx ON statechart_effects (machine, run_after, id);

-- Finds an instance's effects, to cancel one and to keep actions in order.
CREATE INDEX IF NOT EXISTS statechart_effects_instance_idx ON statechart_effects (machine, instance_id, effect_id);
