package smoke

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/target/goalert/test/smoke/harness"
)

// dana is the primary on-call for the "mine" service (step 0) and sits on the
// second step of "escalated", which sam fronts. sam is primary for "theirs".
//
// Every service is low urgency, so nothing pages anyone -- which keeps
// includeNotified out of the picture and leaves being on-call as the only way
// these alerts can reach a list. Neither user has favorited anything.
const onCallServicesSQL = `
	insert into users (id, name, email, role)
	values
		({{uuid "dana"}}, 'dana', 'dana@example.com', 'user'),
		({{uuid "sam"}},  'sam',  'sam@example.com',  'user');

	insert into escalation_policies (id, name, repeat)
	values
		({{uuid "epMine"}},      'mine ep',      0),
		({{uuid "epTheirs"}},    'theirs ep',    0),
		({{uuid "epEscalated"}}, 'escalated ep', 0);

	insert into escalation_policy_steps (id, escalation_policy_id, delay)
	values
		({{uuid "stepMine"}},   {{uuid "epMine"}},   60),
		({{uuid "stepTheirs"}}, {{uuid "epTheirs"}}, 60);

	-- step_number comes from row order, so sam fronts this policy and dana backs it.
	insert into escalation_policy_steps (id, escalation_policy_id, delay)
	values
		({{uuid "escFirst"}},  {{uuid "epEscalated"}}, 60),
		({{uuid "escSecond"}}, {{uuid "epEscalated"}}, 60);

	insert into escalation_policy_actions (escalation_policy_step_id, user_id)
	values
		({{uuid "stepMine"}},   {{uuid "dana"}}),
		({{uuid "stepTheirs"}}, {{uuid "sam"}}),
		({{uuid "escFirst"}},   {{uuid "sam"}}),
		({{uuid "escSecond"}},  {{uuid "dana"}});

	insert into services (id, escalation_policy_id, name, notification_urgency)
	values
		({{uuid "mine"}},      {{uuid "epMine"}},      'my service',      'low'),
		({{uuid "theirs"}},    {{uuid "epTheirs"}},    'their service',   'low'),
		({{uuid "escalated"}}, {{uuid "epEscalated"}}, 'backup service',  'low');

	-- A policy whose first step reaches nobody: the schedule has no rules, so
	-- dana on the step below is the one actually responsible.
	insert into schedules (id, name, time_zone)
	values ({{uuid "uncovered"}}, 'uncovered', 'UTC');

	insert into escalation_policies (id, name, repeat)
	values ({{uuid "epGap"}}, 'gap ep', 0);

	insert into escalation_policy_steps (id, escalation_policy_id, delay)
	values
		({{uuid "gapFirst"}},  {{uuid "epGap"}}, 60),
		({{uuid "gapSecond"}}, {{uuid "epGap"}}, 60);

	insert into escalation_policy_actions (escalation_policy_step_id, schedule_id)
	values ({{uuid "gapFirst"}}, {{uuid "uncovered"}});

	insert into escalation_policy_actions (escalation_policy_step_id, user_id)
	values ({{uuid "gapSecond"}}, {{uuid "dana"}});

	insert into services (id, escalation_policy_id, name, notification_urgency)
	values ({{uuid "gap"}}, {{uuid "epGap"}}, 'gap service', 'low');
`

// overviewAlertIDs runs the alert list the way the home page does. escPath is
// the opt-in "services I am on the escalation path for" filter.
func overviewAlertIDs(t *testing.T, h *harness.Harness, userID, status string, escPath bool) []int {
	t.Helper()
	return alertIDs(t, h, userID, fmt.Sprintf(`
		favoritesOnly: true,
		includeNotified: true,
		includeAssigned: true,
		includeOnCallServices: true,
		includeEscalationPathServices: %t,
		filterByStatus: [%s],
		first: 100
	`, escPath, status))
}

// TestAlertOnCallServices covers an open alert on a service you are on-call
// for that somebody else already holds.
//
// A favorite, a notification you received, and an alert resolving to you are
// the other three routes onto the home page, and a claimed alert on a service
// you have not favorited satisfies none of them. Being on-call for the service
// has to be enough on its own, or whoever comes on-call next cannot see what
// they just became accountable for.
func TestAlertOnCallServices(t *testing.T) {
	t.Parallel()

	h := harness.NewHarness(t, onCallServicesSQL, "add-service-alert-schedule")
	defer h.Close()

	h.SetConfigValue("General.EnableAlertAssignment", "true")

	// Let the engine work out who is on-call.
	h.Trigger()

	claimed := h.CreateAlert(h.UUID("mine"), "claimed by sam")
	unclaimed := h.CreateAlert(h.UUID("mine"), "nobody has this")
	theirs := h.CreateAlert(h.UUID("theirs"), "not dana's service")
	h.Trigger()

	// sam takes the alert on dana's service, as the previous rotation would have.
	mutateAlerts(t, h, h.UUID("dana"),
		fmt.Sprintf(`{ alertIDs: [%d], assignedUserID: "%s" }`, claimed.ID(), h.UUID("sam")))

	assert.ElementsMatch(t,
		[]int{claimed.ID(), unclaimed.ID()},
		overviewAlertIDs(t, h, h.UUID("dana"), "StatusUnacknowledged", false),
		"dana should see everything on the service she is on-call for, claimed or not")

	// sam still sees what he holds plus his own service, and not dana's
	// unclaimed alert.
	samSees := overviewAlertIDs(t, h, h.UUID("sam"), "StatusUnacknowledged", false)
	assert.Contains(t, samSees, claimed.ID(), "sam should keep the alert he claimed")
	assert.Contains(t, samSees, theirs.ID(), "sam should see his own service")
	assert.NotContains(t, samSees, unclaimed.ID(),
		"sam is not on-call for dana's service and holds nothing there")
}

// TestAlertOnCallEscalationPath separates the two ideas the home page needs to
// keep apart: what is yours now, and what could reach you if an alert escalates.
//
// dana is primary for "mine" and second on the policy behind "backup service".
// The default list is what she is responsible for; the opt-in filter is what
// might land on her later.
func TestAlertOnCallEscalationPath(t *testing.T) {
	t.Parallel()

	h := harness.NewHarness(t, onCallServicesSQL, "add-service-alert-schedule")
	defer h.Close()

	h.SetConfigValue("General.EnableAlertAssignment", "true")

	h.Trigger()

	mine := h.CreateAlert(h.UUID("mine"), "primary")
	backup := h.CreateAlert(h.UUID("escalated"), "would escalate to dana")
	h.Trigger()

	assert.ElementsMatch(t, []int{mine.ID()},
		overviewAlertIDs(t, h, h.UUID("dana"), "StatusUnacknowledged", false),
		"being further down a policy is not being on-call for the service")

	assert.ElementsMatch(t, []int{mine.ID(), backup.ID()},
		overviewAlertIDs(t, h, h.UUID("dana"), "StatusUnacknowledged", true),
		"opting in should add services that would only reach her on escalation")

	// sam fronts that policy, so it is his by default with no opt-in needed.
	assert.Contains(t,
		overviewAlertIDs(t, h, h.UUID("sam"), "StatusUnacknowledged", false), backup.ID(),
		"the primary on-call should see it without opting in")
}

// TestAlertOnCallServicesRespectsStatus pins that the status tabs still apply:
// the on-call branch widens which services are in scope, not which statuses.
func TestAlertOnCallServicesRespectsStatus(t *testing.T) {
	t.Parallel()

	h := harness.NewHarness(t, onCallServicesSQL, "add-service-alert-schedule")
	defer h.Close()

	h.SetConfigValue("General.EnableAlertAssignment", "true")
	h.Trigger()

	open := h.CreateAlert(h.UUID("mine"), "still open")
	acked := h.CreateAlert(h.UUID("mine"), "already acked")
	h.Trigger()

	mutateAlerts(t, h, h.UUID("sam"),
		fmt.Sprintf(`{ alertIDs: [%d], newStatus: StatusAcknowledged }`, acked.ID()))

	assert.ElementsMatch(t, []int{open.ID()},
		overviewAlertIDs(t, h, h.UUID("dana"), "StatusUnacknowledged", false),
		"the unacknowledged tab must exclude the acked alert")
	assert.ElementsMatch(t, []int{acked.ID()},
		overviewAlertIDs(t, h, h.UUID("dana"), "StatusAcknowledged", false),
		"the acknowledged tab must show only the acked alert")
	assert.ElementsMatch(t, []int{open.ID(), acked.ID()},
		overviewAlertIDs(t, h, h.UUID("dana"), "StatusUnacknowledged, StatusAcknowledged", false),
		"the active tab must show both")
}

// TestAlertOnCallServicesDisabled pins the kill switch: with
// General.DisableOnCallServiceAlerts set, being on-call stops widening the
// list and it falls back to favorites, notifications and assignment.
//
// The option is refused at the resolver rather than the UI, so a client that
// keeps sending it cannot re-enable the behavior.
func TestAlertOnCallServicesDisabled(t *testing.T) {
	t.Parallel()

	h := harness.NewHarness(t, onCallServicesSQL, "add-service-alert-schedule")
	defer h.Close()

	h.SetConfigValue("General.EnableAlertAssignment", "true")
	h.SetConfigValue("General.DisableOnCallServiceAlerts", "true")

	h.Trigger()

	claimed := h.CreateAlert(h.UUID("mine"), "claimed by sam")
	escalated := h.CreateAlert(h.UUID("escalated"), "would escalate to dana")
	h.Trigger()

	mutateAlerts(t, h, h.UUID("dana"),
		fmt.Sprintf(`{ alertIDs: [%d], assignedUserID: "%s" }`, claimed.ID(), h.UUID("sam")))

	assert.Empty(t, overviewAlertIDs(t, h, h.UUID("dana"), "StatusUnacknowledged", false),
		"being on-call should not widen the list while disabled")
	assert.Empty(t, overviewAlertIDs(t, h, h.UUID("dana"), "StatusUnacknowledged", true),
		"the escalation path opt-in must not bypass the config key")

	// Assignment is a separate route and keeps working: sam holds the claimed
	// alert outright, and fronts the policy the escalated one sits on.
	assert.ElementsMatch(t, []int{claimed.ID(), escalated.ID()},
		assignedAlertIDs(t, h, h.UUID("sam")),
		"disabling on-call visibility must not affect the assigned filter")
}

// TestAlertOnCallUnstaffedFirstStep covers a service whose first escalation
// step reaches nobody -- an uncovered rotation, or a step targeting only a
// notification channel.
//
// Nobody is on-call for step 0, so resolving the primary against that step
// literally put the service in nobody's list. The person on the step below is
// who the alert actually falls to, and an explicit assignment must not hide it
// from them: acknowledging freezes ownership to the acknowledger, which would
// otherwise close the last route by which the responsible person could see it.
func TestAlertOnCallUnstaffedFirstStep(t *testing.T) {
	t.Parallel()

	h := harness.NewHarness(t, onCallServicesSQL, "add-service-alert-schedule")
	defer h.Close()

	h.SetConfigValue("General.EnableAlertAssignment", "true")
	h.Trigger()

	gap := h.CreateAlert(h.UUID("gap"), "nobody above me")
	h.Trigger()

	assert.Contains(t, overviewAlertIDs(t, h, h.UUID("dana"), "StatusUnacknowledged", false), gap.ID(),
		"the first step reaches nobody, so the step below is on-call for the service")

	// sam is on no step of this policy and claims it anyway.
	mutateAlerts(t, h, h.UUID("sam"),
		fmt.Sprintf(`{ alertIDs: [%d], newStatus: StatusAcknowledged }`, gap.ID()))

	assert.Contains(t, overviewAlertIDs(t, h, h.UUID("dana"), "StatusAcknowledged", false), gap.ID(),
		"somebody else acknowledging it must not remove it from the on-call user's list")
	// sam keeps it too, by virtue of holding it rather than being on-call.
	assert.Contains(t, overviewAlertIDs(t, h, h.UUID("sam"), "StatusAcknowledged", false), gap.ID(),
		"the acknowledger should still see what they claimed")
}
