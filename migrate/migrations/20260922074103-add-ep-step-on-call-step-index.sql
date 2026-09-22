-- +migrate Up notransaction

-- Alert ownership is resolved by looking up the on-call users of an alert's
-- remaining escalation steps. That lookup filters on ep_step_id, and the only
-- index on the table leads with user_id, so it degraded into scanning that
-- index for every step of every alert on the list.
--
-- The alert list resolves ownership for every row it returns, so this is on the
-- hot path for the default /alerts view.
--
-- CONCURRENTLY so building it cannot lock out the escalation manager, which
-- rewrites this table every engine tick.
DROP INDEX IF EXISTS idx_ep_step_on_call_step;
CREATE INDEX CONCURRENTLY idx_ep_step_on_call_step
    ON ep_step_on_call_users (ep_step_id)
    WHERE end_time IS NULL;

-- +migrate Down notransaction

DROP INDEX IF EXISTS idx_ep_step_on_call_step;
