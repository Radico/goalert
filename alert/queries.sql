-- name: Alert_LockOneAlertService :one
-- Locks the service associated with the alert.
SELECT
    maintenance_expires_at NOTNULL::bool AS is_maint_mode,
    alerts.status
FROM
    services svc
    JOIN alerts ON alerts.service_id = svc.id
WHERE
    alerts.id = $1
FOR UPDATE;

-- name: Alert_RequestAlertEscalationByTime :one
-- Returns the alert ID and the escalation policy ID for the alert that should be escalated based on the provided time.
UPDATE
    escalation_policy_state
SET
    force_escalation = TRUE
WHERE
    alert_id = $1
    AND (last_escalation <= $2::timestamptz
        OR last_escalation IS NULL)
RETURNING
    TRUE;

-- name: Alert_AlertHasEPState :one
-- Returns true if the alert has an escalation policy state.
SELECT
    EXISTS (
        SELECT
            1
        FROM
            escalation_policy_state
        WHERE
            alert_id = $1) AS has_ep_state;

-- name: Alert_GetAlertFeedback :many
-- Returns the noise reason for the alert.
SELECT
    alert_id,
    noise_reason
FROM
    alert_feedback
WHERE
    alert_id = ANY ($1::int[]);

-- name: Alert_SetAlertFeedback :exec
-- Sets the noise reason for the alert.
INSERT INTO alert_feedback(alert_id, noise_reason)
    VALUES ($1, $2)
ON CONFLICT (alert_id)
    DO UPDATE SET
        noise_reason = $2
    WHERE
        alert_feedback.alert_id = $1;

-- name: Alert_SetManyAlertFeedback :many
-- Sets the noise reason for many alerts.
INSERT INTO alert_feedback(alert_id, noise_reason)
    VALUES (unnest(@alert_ids::bigint[]), @noise_reason)
ON CONFLICT (alert_id)
    DO UPDATE SET
        noise_reason = excluded.noise_reason
    WHERE
        alert_feedback.alert_id = excluded.alert_id
    RETURNING
        alert_id;

-- name: Alert_GetAlertMetadata :one
-- Returns the metadata for the alert.
SELECT
    metadata
FROM
    alert_data
WHERE
    alert_id = $1;

-- name: Alert_GetAlertManyMetadata :many
-- Returns the metadata for many alerts.
SELECT
    alert_id,
    metadata
FROM
    alert_data
WHERE
    alert_id = ANY (@alert_ids::bigint[]);

-- name: Alert_SetAlertMetadata :execrows
-- Sets the metadata for the alert.
INSERT INTO alert_data(alert_id, metadata)
SELECT
    a.id,
    $2
FROM
    alerts a
WHERE
    a.id = $1
    AND a.status != 'closed'
    AND (a.service_id = $3
        OR $3 IS NULL) -- ensure the alert is associated with the service, if coming from an integration
ON CONFLICT (alert_id)
    DO UPDATE SET
        metadata = $2
    WHERE
        alert_data.alert_id = $1;

-- name: Alert_ServiceEPHasSteps :one
-- Returns true if the Escalation Policy for the provided service has at least one step.
SELECT
    EXISTS (
        SELECT
            1
        FROM
            escalation_policy_steps step
            JOIN services svc ON step.escalation_policy_id = svc.escalation_policy_id
        WHERE
            svc.id = @service_id);

-- name: Alert_LockService :exec
-- Locks the service associated with the alert.
SELECT
    1
FROM
    services
WHERE
    id = @service_id
FOR UPDATE;

-- name: Alert_LockManyAlertServices :exec
-- Locks the service(s) associated with the specified alerts.
SELECT
    1
FROM
    alerts a
    JOIN services s ON a.service_id = s.id
WHERE
    a.id = ANY (@alert_ids::bigint[])
FOR UPDATE;

-- name: Alert_GetStatusAndLockService :one
-- Returns the status of the alert and locks the service associated with the alert.
SELECT
    a.status
FROM
    alerts a
    JOIN services svc ON svc.id = a.service_id
WHERE
    a.id = @id::bigint
FOR UPDATE;

-- name: Alert_GetEscalationPolicyID :one
-- Returns the escalation policy ID associated with the alert.
SELECT
    escalation_policy_id
FROM
    alerts a
    JOIN services svc ON svc.id = a.service_id
WHERE
    a.id = @id::bigint;

-- name: Alert_GetManyExplicitAssignees :many
-- Returns the explicitly-assigned (claimed) user for many alerts.
--
-- An alert with assigned_user_id set has been claimed -- either by
-- acknowledging it or by an explicit re-assignment -- and is frozen: it no
-- longer follows escalations or rotation handoffs.
SELECT
    id AS alert_id,
    assigned_user_id
FROM
    alerts
WHERE
    id = ANY (@alert_ids::bigint[])
    AND assigned_user_id NOTNULL;

-- name: Alert_GetManyOnCallAssignees :many
-- Returns the derived assignee for many unclaimed alerts.
--
-- Unclaimed alerts (assigned_user_id ISNULL) resolve to whoever is currently
-- on-call for the alert's current escalation step, so ownership follows
-- escalations and rotation handoffs without any writes.
--
-- Joining on escalation_policy_step_number (NOT NULL, default 0) rather than
-- escalation_policy_step_id (NULL until the first escalation) means a brand new
-- alert correctly resolves to step 0's on-call user.
--
-- A step can resolve to several users (overlapping schedule rules, or an
-- override that adds without removing); DISTINCT ON takes the longest-serving.
-- Alerts whose step resolves to nobody -- for example a step targeting only a
-- notification channel -- return no row at all, and are therefore unassigned.
SELECT DISTINCT ON (st.alert_id)
    st.alert_id,
    ocu.user_id
FROM
    escalation_policy_state st
    JOIN alerts a ON a.id = st.alert_id
        AND a.assigned_user_id ISNULL
    JOIN escalation_policy_steps step ON step.escalation_policy_id = st.escalation_policy_id
        AND step.step_number = st.escalation_policy_step_number
    JOIN ep_step_on_call_users ocu ON ocu.ep_step_id = step.id
        AND ocu.end_time ISNULL
WHERE
    st.alert_id = ANY (@alert_ids::bigint[])
ORDER BY
    st.alert_id,
    ocu.start_time;

-- name: Alert_SetManyAlertAssignees :many
-- Explicitly assigns many alerts to a user (or clears the assignment when NULL).
UPDATE
    alerts
SET
    assigned_user_id = @assigned_user_id
WHERE
    id = ANY (@alert_ids::bigint[])
    AND status != 'closed'
    AND assigned_user_id IS DISTINCT FROM @assigned_user_id
RETURNING
    id;

-- name: Alert_ClaimManyAlertsOnAck :many
-- Claims ownership of alerts for the acknowledging user.
--
-- Only unclaimed alerts are affected, so re-acknowledging an alert can never
-- take it away from whoever already owns it.
UPDATE
    alerts
SET
    assigned_user_id = @assigned_user_id
WHERE
    id = ANY (@alert_ids::bigint[])
    AND assigned_user_id ISNULL
RETURNING
    id;

