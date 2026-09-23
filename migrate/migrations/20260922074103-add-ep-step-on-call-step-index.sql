-- +migrate Up notransaction

-- Alert ownership is resolved by looking up the on-call users of an alert's
-- remaining escalation steps. That lookup filters on ep_step_id, and neither
-- existing index can serve it: one is on (id), the other leads with user_id.
--
-- The alert list resolves ownership for every row it returns, so this is on the
-- hot path for the default /alerts view.
--
-- CONCURRENTLY so neither statement locks out the escalation manager, which
-- rewrites this table every engine tick. Dropping first rather than using
-- IF NOT EXISTS keeps a retry self-healing: a cancelled concurrent build leaves
-- the index behind and INVALID, and IF NOT EXISTS would skip past it forever.
DROP INDEX CONCURRENTLY IF EXISTS idx_ep_step_on_call_step;
CREATE INDEX CONCURRENTLY idx_ep_step_on_call_step
    ON ep_step_on_call_users (ep_step_id)
    WHERE end_time IS NULL;

-- +migrate Down notransaction

DROP INDEX CONCURRENTLY IF EXISTS idx_ep_step_on_call_step;
