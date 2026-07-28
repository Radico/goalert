-- +migrate Up

-- assigned_user_id records explicit ownership of an alert.
--
-- NULL means "unclaimed" -- the effective assignee is derived at read time from
-- whoever is currently on-call for the alert's current escalation step. It is
-- only written when a user acknowledges the alert (claiming it) or when someone
-- explicitly re-assigns it. Deriving rather than storing the on-call user avoids
-- rewriting every open alert on each rotation handoff.
--
-- This is triage metadata only: it never affects notification routing.
--
-- SET NULL, not CASCADE: deleting a user must not delete their alerts.
ALTER TABLE alerts ADD COLUMN assigned_user_id UUID
    REFERENCES users(id) ON DELETE SET NULL;

CREATE INDEX idx_alerts_assigned_user ON alerts (assigned_user_id)
    WHERE assigned_user_id IS NOT NULL;

-- +migrate Down

DROP INDEX idx_alerts_assigned_user;
ALTER TABLE alerts DROP COLUMN assigned_user_id;
