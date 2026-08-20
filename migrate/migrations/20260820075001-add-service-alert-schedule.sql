-- +migrate Up

-- Schedule-based alerting: a service with alerting windows configured notifies
-- only during them. Outside them, alerts are still created and tracked -- the
-- escalation policy simply never runs, so an alert captured off-hours notifies
-- when the window next opens.
--
-- This is orthogonal to maintenance mode: maintenance is a manual, time-boxed
-- mute, while this is a recurring weekly schedule.
ALTER TABLE services ADD COLUMN alert_schedule_enabled BOOLEAN NOT NULL DEFAULT FALSE;

-- The IANA zone the windows are interpreted in.
ALTER TABLE services ADD COLUMN notification_time_zone TEXT;

-- Materialized by the escalation manager each engine tick. Evaluating the
-- weekly window in Go (schedule/rule.Rule.IsActive) keeps a single, DST-correct
-- implementation of the predicate and prevents one bad timezone from aborting
-- the whole escalation batch.
ALTER TABLE services ADD COLUMN notification_suppressed BOOLEAN NOT NULL DEFAULT FALSE;

-- The engine loads this zone every pass to decide whether a window is open. A
-- NULL would leave the service permanently suppressed, logging on every tick.
ALTER TABLE services ADD CONSTRAINT services_notification_time_zone_check
    CHECK (NOT alert_schedule_enabled OR notification_time_zone NOTNULL);

CREATE TABLE service_notification_rules (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    service_id UUID NOT NULL REFERENCES services (id) ON DELETE CASCADE,
    start_time TIME NOT NULL DEFAULT '00:00:00',
    end_time TIME NOT NULL DEFAULT '00:00:00',
    sunday BOOLEAN NOT NULL DEFAULT FALSE,
    monday BOOLEAN NOT NULL DEFAULT FALSE,
    tuesday BOOLEAN NOT NULL DEFAULT FALSE,
    wednesday BOOLEAN NOT NULL DEFAULT FALSE,
    thursday BOOLEAN NOT NULL DEFAULT FALSE,
    friday BOOLEAN NOT NULL DEFAULT FALSE,
    saturday BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_service_notification_rules_service ON service_notification_rules (service_id);

-- The escalation manager touches only these rows each tick. Without this it
-- would seq-scan every service every 5 seconds on installs using no windows.
CREATE INDEX idx_services_alert_schedule ON services (id)
    WHERE alert_schedule_enabled OR notification_suppressed;

UPDATE engine_processing_versions SET "version" = 5 WHERE type_id = 'escalation';

-- +migrate Down

UPDATE engine_processing_versions SET "version" = 4 WHERE type_id = 'escalation';

DROP TABLE service_notification_rules;

DROP INDEX idx_services_alert_schedule;

ALTER TABLE services DROP CONSTRAINT services_notification_time_zone_check;

ALTER TABLE services DROP COLUMN notification_suppressed;
ALTER TABLE services DROP COLUMN notification_time_zone;
ALTER TABLE services DROP COLUMN alert_schedule_enabled;
