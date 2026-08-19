-- +migrate Up

-- Schedule-based alerting: a service in 'scheduled' urgency notifies only
-- during its configured weekly windows. Outside them, alerts are still
-- created and tracked -- the escalation policy simply never runs.
ALTER TABLE services ADD COLUMN notification_time_zone TEXT;

-- Materialized by the escalation manager each engine tick. Evaluating the
-- weekly window in Go (schedule/rule.Rule.IsActive) keeps a single, DST-correct
-- implementation of the predicate and prevents one bad timezone from aborting
-- the whole escalation batch.
ALTER TABLE services ADD COLUMN notification_suppressed BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE services DROP CONSTRAINT services_notification_urgency_check;
ALTER TABLE services ADD CONSTRAINT services_notification_urgency_check
    CHECK (notification_urgency IN ('high', 'low', 'scheduled'));

-- The engine loads this zone every pass to decide whether a window is open. A
-- NULL would leave the service permanently suppressed, logging on every tick.
ALTER TABLE services ADD CONSTRAINT services_notification_time_zone_check
    CHECK (notification_urgency != 'scheduled' OR notification_time_zone NOTNULL);

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
    WHERE notification_urgency = 'scheduled' OR notification_suppressed;

UPDATE engine_processing_versions SET "version" = 6 WHERE type_id = 'escalation';

-- +migrate Down

UPDATE engine_processing_versions SET "version" = 5 WHERE type_id = 'escalation';

DROP TABLE service_notification_rules;

DROP INDEX idx_services_alert_schedule;

ALTER TABLE services DROP CONSTRAINT services_notification_time_zone_check;

-- Fail loud, not silent. These services were paging on a schedule, and after a
-- rollback there is no way to express that. For an alerting system over-paging
-- is recoverable, while silently never paging again is not.
UPDATE services SET notification_urgency = 'high' WHERE notification_urgency = 'scheduled';

ALTER TABLE services DROP CONSTRAINT services_notification_urgency_check;
ALTER TABLE services ADD CONSTRAINT services_notification_urgency_check
    CHECK (notification_urgency IN ('high', 'low'));

ALTER TABLE services DROP COLUMN notification_suppressed;
ALTER TABLE services DROP COLUMN notification_time_zone;
