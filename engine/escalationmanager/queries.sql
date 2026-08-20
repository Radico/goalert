-- name: EscMgrScheduledServices :many
-- Returns the weekly alerting windows of every service using schedule-based
-- alerting. A service with no rules is simply absent: it can never be open, and
-- EscMgrSetSuppressedServices suppresses anything not reported as open.
SELECT
    svc.id AS service_id,
    svc.notification_time_zone,
    r.start_time,
    r.end_time,
    r.sunday,
    r.monday,
    r.tuesday,
    r.wednesday,
    r.thursday,
    r.friday,
    r.saturday
FROM
    services svc
    JOIN service_notification_rules r ON r.service_id = svc.id
WHERE
    svc.alert_schedule_enabled;

-- name: EscMgrSetSuppressedServices :exec
-- Materializes the current alerting-window state. @open_service_ids is the set
-- of schedule-enabled services whose window is open right now; every other
-- schedule-enabled service is suppressed, and every other service is not.
--
-- The IS DISTINCT FROM guard means the steady state writes no rows at all.
-- The leading predicate restricts this to idx_services_alert_schedule: any
-- other service is neither scheduled nor suppressed, so its value is already
-- false and cannot change.
UPDATE
    services
SET
    notification_suppressed = (alert_schedule_enabled
        AND NOT (id = ANY (@open_service_ids::uuid[])))
WHERE (alert_schedule_enabled
    OR notification_suppressed)
AND notification_suppressed IS DISTINCT FROM (alert_schedule_enabled
    AND NOT (id = ANY (@open_service_ids::uuid[])));
