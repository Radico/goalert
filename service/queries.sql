-- name: ServiceNotificationRules_FindMany :many
-- Returns the alerting windows for the given services, oldest first.
SELECT
    service_id,
    start_time,
    end_time,
    sunday,
    monday,
    tuesday,
    wednesday,
    thursday,
    friday,
    saturday
FROM
    service_notification_rules
WHERE
    service_id = ANY (@service_ids::uuid[])
ORDER BY
    service_id,
    created_at,
    id;

-- name: ServiceNotificationRules_DeleteByService :exec
DELETE FROM service_notification_rules
WHERE service_id = $1;

-- name: ServiceNotificationRules_Insert :exec
INSERT INTO service_notification_rules (service_id, start_time, end_time, sunday, monday, tuesday, wednesday, thursday, friday, saturday)
    VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10);
