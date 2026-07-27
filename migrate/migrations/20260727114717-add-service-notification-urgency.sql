-- +migrate Up
UPDATE engine_processing_versions SET "version" = 5 WHERE type_id = 'escalation';
ALTER TABLE services ADD COLUMN notification_urgency TEXT NOT NULL DEFAULT 'high';
ALTER TABLE services ADD CONSTRAINT services_notification_urgency_check CHECK (notification_urgency IN ('high', 'low'));

-- +migrate Down
UPDATE engine_processing_versions SET "version" = 4 WHERE type_id = 'escalation';
ALTER TABLE services DROP CONSTRAINT services_notification_urgency_check;
ALTER TABLE services DROP COLUMN notification_urgency;
