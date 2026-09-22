package smoke

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/target/goalert/expflag"
	"github.com/target/goalert/test/smoke/harness"
)

// twoStepEPSQL builds an escalation policy with two steps -- step 0 targeting
// user1 and step 1 targeting user3 -- plus a service using it.
//
// user2 is deliberately on neither step, so tests can acknowledge or assign as
// somebody who is not on-call and still make unambiguous assertions.
const twoStepEPSQL = `
	insert into users (id, name, email, role)
	values
		({{uuid "user1"}}, 'bob', 'bob@example.com', 'user'),
		({{uuid "user2"}}, 'ann', 'ann@example.com', 'user'),
		({{uuid "user3"}}, 'joe', 'joe@example.com', 'user');

	insert into user_contact_methods (id, user_id, name, type, value)
	values
		({{uuid "cm1"}}, {{uuid "user1"}}, 'personal', 'SMS', {{phone "1"}}),
		({{uuid "cm3"}}, {{uuid "user3"}}, 'personal', 'SMS', {{phone "3"}});

	insert into user_notification_rules (user_id, contact_method_id, delay_minutes)
	values
		({{uuid "user1"}}, {{uuid "cm1"}}, 0),
		({{uuid "user3"}}, {{uuid "cm3"}}, 0);

	insert into escalation_policies (id, name, repeat)
	values ({{uuid "eid"}}, 'esc policy', 0);

	insert into escalation_policy_steps (id, escalation_policy_id, delay, step_number)
	values
		({{uuid "es1"}}, {{uuid "eid"}}, 60, 0),
		({{uuid "es2"}}, {{uuid "eid"}}, 60, 1);

	insert into escalation_policy_actions (escalation_policy_step_id, user_id)
	values
		({{uuid "es1"}}, {{uuid "user1"}}),
		({{uuid "es2"}}, {{uuid "user3"}});

	insert into services (id, escalation_policy_id, name)
	values ({{uuid "sid"}}, {{uuid "eid"}}, 'service');`

type assignmentResult struct {
	Alert struct {
		AssignedUser *struct {
			ID   string
			Name string
		}
		AssignmentSource string
	}
}

// queryAssignment reads the resolved assignment for an alert.
func queryAssignment(t *testing.T, h *harness.Harness, alertID int) assignmentResult {
	t.Helper()

	g := h.GraphQLQueryUserT(t, h.UUID("user1"), fmt.Sprintf(`
		query {
			alert(id: %d) {
				assignedUser { id name }
				assignmentSource
			}
		}
	`, alertID))
	for _, err := range g.Errors {
		t.Fatal("GraphQL Error:", err.Message)
	}

	var res assignmentResult
	require.NoError(t, json.Unmarshal(g.Data, &res))
	return res
}

// mutateAlerts runs an updateAlerts mutation as the given user, failing on any
// GraphQL error.
func mutateAlerts(t *testing.T, h *harness.Harness, asUser, input string) {
	t.Helper()

	g := h.GraphQLQueryUserT(t, asUser, fmt.Sprintf(`
		mutation {
			updateAlerts(input: %s) { id }
		}
	`, input))
	for _, err := range g.Errors {
		t.Fatal("GraphQL Error:", err.Message)
	}
}

// TestAlertAssignmentOnCall verifies that an unclaimed alert is attributed to
// whoever is on-call for its *current* escalation step, and that the attribution
// follows the alert as it escalates -- without anything being written to the
// alert.
func TestAlertAssignmentOnCall(t *testing.T) {
	t.Parallel()

	h := harness.NewHarness(t, twoStepEPSQL, "add-alert-assigned-user")
	defer h.Close()

	h.SetConfigValue("General.EnableAlertAssignment", "true")

	d1 := h.Twilio(t).Device(h.Phone("1"))
	d3 := h.Twilio(t).Device(h.Phone("3"))

	a := h.CreateAlert(h.UUID("sid"), "step zero")
	d1.ExpectSMS("step zero")

	// unclaimed: derived from step 0's on-call user
	res := queryAssignment(t, h, a.ID())
	assert.Equal(t, "onCall", res.Alert.AssignmentSource)
	require.NotNil(t, res.Alert.AssignedUser, "expected a derived assignee")
	assert.Equal(t, h.UUID("user1"), res.Alert.AssignedUser.ID)

	// escalate to step 1
	h.FastForward(time.Hour)
	h.Trigger()
	d3.ExpectSMS("step zero")

	// attribution follows the escalation, with no write to the alert
	res = queryAssignment(t, h, a.ID())
	assert.Equal(t, "onCall", res.Alert.AssignmentSource)
	require.NotNil(t, res.Alert.AssignedUser)
	assert.Equal(t, h.UUID("user3"), res.Alert.AssignedUser.ID,
		"assignee should follow the alert to step 1's on-call user")
}

// TestAlertAssignmentAckFreezes verifies that acknowledging an alert claims
// ownership for the acking user, and that the claim is frozen -- escalating
// afterwards no longer moves it.
func TestAlertAssignmentAckFreezes(t *testing.T) {
	t.Parallel()

	h := harness.NewHarness(t, twoStepEPSQL, "add-alert-assigned-user")
	defer h.Close()

	h.SetConfigValue("General.EnableAlertAssignment", "true")

	d1 := h.Twilio(t).Device(h.Phone("1"))
	d3 := h.Twilio(t).Device(h.Phone("3"))

	a := h.CreateAlert(h.UUID("sid"), "claim me")
	d1.ExpectSMS("claim me")

	// user2 acks, even though user1 is the on-call user
	mutateAlerts(t, h, h.UUID("user2"),
		fmt.Sprintf(`{ alertIDs: [%d], newStatus: StatusAcknowledged }`, a.ID()))

	res := queryAssignment(t, h, a.ID())
	assert.Equal(t, "explicit", res.Alert.AssignmentSource)
	require.NotNil(t, res.Alert.AssignedUser)
	assert.Equal(t, h.UUID("user2"), res.Alert.AssignedUser.ID,
		"acknowledging should claim the alert for the acking user")

	// Escalating moves the alert to step 1 (user3's step). Note that this still
	// notifies user3 even though the alert is acknowledged and owned by user2 --
	// ownership is triage metadata and never gates notification routing.
	h.Escalate(a.ID(), 0)
	d3.ExpectSMS("claim me")

	res = queryAssignment(t, h, a.ID())
	assert.Equal(t, "explicit", res.Alert.AssignmentSource)
	require.NotNil(t, res.Alert.AssignedUser)
	assert.Equal(t, h.UUID("user2"), res.Alert.AssignedUser.ID,
		"an explicit assignment must not follow escalation")
}

// TestAlertAssignmentReacked verifies that re-acknowledging an already-claimed
// alert cannot take it away from its owner.
func TestAlertAssignmentReacked(t *testing.T) {
	t.Parallel()

	h := harness.NewHarness(t, twoStepEPSQL, "add-alert-assigned-user")
	defer h.Close()

	h.SetConfigValue("General.EnableAlertAssignment", "true")

	d1 := h.Twilio(t).Device(h.Phone("1"))

	a := h.CreateAlert(h.UUID("sid"), "mine")
	d1.ExpectSMS("mine")

	mutateAlerts(t, h, h.UUID("user2"),
		fmt.Sprintf(`{ alertIDs: [%d], newStatus: StatusAcknowledged }`, a.ID()))
	mutateAlerts(t, h, h.UUID("user3"),
		fmt.Sprintf(`{ alertIDs: [%d], newStatus: StatusAcknowledged }`, a.ID()))

	res := queryAssignment(t, h, a.ID())
	assert.Equal(t, "explicit", res.Alert.AssignmentSource)
	require.NotNil(t, res.Alert.AssignedUser)
	assert.Equal(t, h.UUID("user2"), res.Alert.AssignedUser.ID,
		"a redundant ack must not steal ownership")
}

// TestAlertAssignmentReassign verifies explicit re-assignment and clearing.
func TestAlertAssignmentReassign(t *testing.T) {
	t.Parallel()

	h := harness.NewHarness(t, twoStepEPSQL, "add-alert-assigned-user")
	defer h.Close()

	h.SetConfigValue("General.EnableAlertAssignment", "true")

	d1 := h.Twilio(t).Device(h.Phone("1"))

	a := h.CreateAlert(h.UUID("sid"), "hand off")
	d1.ExpectSMS("hand off")

	// hand the alert to user2, who is not on-call at all
	mutateAlerts(t, h, h.UUID("user1"),
		fmt.Sprintf(`{ alertIDs: [%d], assignedUserID: "%s" }`, a.ID(), h.UUID("user2")))

	res := queryAssignment(t, h, a.ID())
	assert.Equal(t, "explicit", res.Alert.AssignmentSource)
	require.NotNil(t, res.Alert.AssignedUser)
	assert.Equal(t, h.UUID("user2"), res.Alert.AssignedUser.ID)

	// clearing returns it to tracking the on-call user
	mutateAlerts(t, h, h.UUID("user1"),
		fmt.Sprintf(`{ alertIDs: [%d], clearAssignment: true }`, a.ID()))

	res = queryAssignment(t, h, a.ID())
	assert.Equal(t, "onCall", res.Alert.AssignmentSource)
	require.NotNil(t, res.Alert.AssignedUser)
	assert.Equal(t, h.UUID("user1"), res.Alert.AssignedUser.ID,
		"clearing an assignment should fall back to the on-call user")
}

// TestAlertAssignmentDoesNotRoute is the guarantee that makes this feature safe
// to roll out: assigning an alert changes who *owns* it, never who is paged.
func TestAlertAssignmentDoesNotRoute(t *testing.T) {
	t.Parallel()

	h := harness.NewHarness(t, twoStepEPSQL, "add-alert-assigned-user")
	defer h.Close()

	h.SetConfigValue("General.EnableAlertAssignment", "true")

	d1 := h.Twilio(t).Device(h.Phone("1"))

	a := h.CreateAlert(h.UUID("sid"), "routing")

	// assign to user2 -- who has no contact methods and is on no step
	mutateAlerts(t, h, h.UUID("user1"),
		fmt.Sprintf(`{ alertIDs: [%d], assignedUserID: "%s" }`, a.ID(), h.UUID("user2")))

	// user1 is still on-call for step 0, so user1 is still the one paged.
	// The harness fails on unexpected messages, so anything sent elsewhere
	// would fail this test.
	d1.ExpectSMS("routing")
}

// TestAlertAssignmentFilter verifies the "assigned to me" search filter covers
// both claimed alerts and alerts derived from being on-call, and excludes
// alerts belonging to someone else.
func TestAlertAssignmentFilter(t *testing.T) {
	t.Parallel()

	h := harness.NewHarness(t, twoStepEPSQL, "add-alert-assigned-user")
	defer h.Close()

	h.SetConfigValue("General.EnableAlertAssignment", "true")

	d1 := h.Twilio(t).Device(h.Phone("1"))

	derived := h.CreateAlert(h.UUID("sid"), "derived to user1")
	d1.ExpectSMS("derived to user1")

	claimed := h.CreateAlert(h.UUID("sid"), "claimed by user2")
	d1.ExpectSMS("claimed by user2")

	mutateAlerts(t, h, h.UUID("user1"),
		fmt.Sprintf(`{ alertIDs: [%d], assignedUserID: "%s" }`, claimed.ID(), h.UUID("user2")))

	assert.ElementsMatch(t, []int{derived.ID()}, assignedAlertIDs(t, h, h.UUID("user1")),
		"user1 should see only the alert derived from being on-call")
	assert.ElementsMatch(t, []int{claimed.ID()}, assignedAlertIDs(t, h, h.UUID("user2")),
		"user2 should see only the alert they were assigned")
}

// TestAlertAssignmentIncludeAssigned verifies that includeAssigned is additive,
// the same way includeNotified is: it widens a favorites-scoped list to also
// show alerts belonging to you, rather than narrowing the list to only those.
//
// This is what backs the alert list default, so getting it wrong would either
// hide the team's alerts or fail to surface your own.
func TestAlertAssignmentIncludeAssigned(t *testing.T) {
	t.Parallel()

	h := harness.NewHarness(t, twoStepEPSQL, "add-alert-assigned-user")
	defer h.Close()

	h.SetConfigValue("General.EnableAlertAssignment", "true")

	d1 := h.Twilio(t).Device(h.Phone("1"))

	a := h.CreateAlert(h.UUID("sid"), "mine but not favorited")
	d1.ExpectSMS("mine but not favorited")

	// user1 has favorited nothing, so a favorites-scoped list is empty unless
	// something widens it.
	query := func(extra string) []int {
		t.Helper()
		g := h.GraphQLQueryUserT(t, h.UUID("user1"), fmt.Sprintf(`
			query {
				alerts(input: { favoritesOnly: true, first: 100, %s }) {
					nodes { alertID }
				}
			}
		`, extra))
		for _, err := range g.Errors {
			t.Fatal("GraphQL Error:", err.Message)
		}

		var res struct {
			Alerts struct {
				Nodes []struct{ AlertID int }
			}
		}
		require.NoError(t, json.Unmarshal(g.Data, &res))

		ids := make([]int, 0, len(res.Alerts.Nodes))
		for _, n := range res.Alerts.Nodes {
			ids = append(ids, n.AlertID)
		}
		return ids
	}

	assert.Empty(t, query("includeAssigned: false"),
		"favorites-only should be empty when nothing is favorited")
	assert.ElementsMatch(t, []int{a.ID()}, query("includeAssigned: true"),
		"includeAssigned should widen the list to alerts assigned to you")

	// An explicit assignee filter is restrictive and supersedes the additive
	// include, so filtering to somebody else must not fall back to showing
	// your own alerts.
	assert.Empty(t,
		query(fmt.Sprintf(`includeAssigned: true, assignedUserID: "%s"`, h.UUID("user2"))),
		"an explicit assignee filter must take precedence over includeAssigned")
}

// TestAlertAssignmentDisabled verifies the feature ships dark: with the config
// key off, alerts report no assignee and the mutation is rejected.
func TestAlertAssignmentDisabled(t *testing.T) {
	t.Parallel()

	h := harness.NewHarness(t, twoStepEPSQL, "add-alert-assigned-user")
	defer h.Close()

	d1 := h.Twilio(t).Device(h.Phone("1"))

	a := h.CreateAlert(h.UUID("sid"), "disabled")
	d1.ExpectSMS("disabled")

	res := queryAssignment(t, h, a.ID())
	assert.Equal(t, "unassigned", res.Alert.AssignmentSource)
	assert.Nil(t, res.Alert.AssignedUser, "no assignee should be reported while disabled")

	g := h.GraphQLQueryUserT(t, h.UUID("user1"), fmt.Sprintf(`
		mutation {
			updateAlerts(input: { alertIDs: [%d], assignedUserID: "%s" }) { id }
		}
	`, a.ID(), h.UUID("user2")))
	assert.NotEmpty(t, g.Errors, "assigning should be rejected while disabled")
}

// TestAlertAssignmentNoOnCall verifies that an alert whose escalation step
// resolves to nobody -- here a step targeting only a notification channel --
// reports as unassigned rather than erroring.
func TestAlertAssignmentNoOnCall(t *testing.T) {
	t.Parallel()

	const initSQL = `
	insert into users (id, name, email, role)
	values ({{uuid "user1"}}, 'bob', 'bob@example.com', 'user');

	insert into notification_channels (id, type, name, value)
	values ({{uuid "chan"}}, 'SLACK', '#test', {{slackChannelID "test"}});

	insert into escalation_policies (id, name)
	values ({{uuid "eid"}}, 'esc policy');

	insert into escalation_policy_steps (id, escalation_policy_id, delay)
	values ({{uuid "es1"}}, {{uuid "eid"}}, 60);

	insert into escalation_policy_actions (escalation_policy_step_id, channel_id)
	values ({{uuid "es1"}}, {{uuid "chan"}});

	insert into services (id, escalation_policy_id, name)
	values ({{uuid "sid"}}, {{uuid "eid"}}, 'service');`

	h := harness.NewHarness(t, initSQL, "add-alert-assigned-user")
	defer h.Close()

	h.SetConfigValue("General.EnableAlertAssignment", "true")

	a := h.CreateAlert(h.UUID("sid"), "channel only")
	h.Slack().Channel("test").ExpectMessage("channel only")

	res := queryAssignment(t, h, a.ID())
	assert.Equal(t, "unassigned", res.Alert.AssignmentSource)
	assert.Nil(t, res.Alert.AssignedUser)
}

// emptyFirstStepEPSQL builds a policy whose first step targets a schedule with
// no rules -- nobody is ever on-call for it -- and whose second step targets
// user1. This is the shape of a real policy with an uncovered rotation, or one
// that opens with a notification channel.
//
// Services for each urgency share the policy; the scheduled one has a window
// that opens two hours out, so it starts closed.
const emptyFirstStepEPSQL = `
	insert into users (id, name, email, role)
	values ({{uuid "user1"}}, 'bob', 'bob@example.com', 'user');

	insert into user_contact_methods (id, user_id, name, type, value)
	values ({{uuid "cm1"}}, {{uuid "user1"}}, 'personal', 'SMS', {{phone "1"}});

	insert into user_notification_rules (user_id, contact_method_id, delay_minutes)
	values ({{uuid "user1"}}, {{uuid "cm1"}}, 0);

	insert into schedules (id, name, time_zone)
	values ({{uuid "empty"}}, 'uncovered', 'UTC');

	insert into escalation_policies (id, name, repeat)
	values ({{uuid "eid"}}, 'esc policy', 0);

	-- step_number is assigned by fn_inc_ep_step_number_on_insert from the count
	-- of existing steps, so row order here is what puts the empty step first.
	--
	-- skip_if_empty on the first step does nothing while a service is
	-- suppressed -- the escalation queries exclude it entirely -- but once the
	-- window opens it is what carries the alert past the uncovered schedule to
	-- somebody who can be paged, inside the window rather than an hour later.
	insert into escalation_policy_steps (id, escalation_policy_id, delay, skip_if_empty)
	values
		({{uuid "es1"}}, {{uuid "eid"}}, 60, true),
		({{uuid "es2"}}, {{uuid "eid"}}, 60, false);

	insert into escalation_policy_actions (escalation_policy_step_id, schedule_id)
	values ({{uuid "es1"}}, {{uuid "empty"}});

	insert into escalation_policy_actions (escalation_policy_step_id, user_id)
	values ({{uuid "es2"}}, {{uuid "user1"}});

	insert into services (id, escalation_policy_id, name, notification_urgency)
	values ({{uuid "low"}}, {{uuid "eid"}}, 'low svc', 'low');

	insert into services (id, escalation_policy_id, name, notification_urgency, notification_time_zone)
	values ({{uuid "sched"}}, {{uuid "eid"}}, 'sched svc', 'scheduled', 'UTC');

	insert into service_notification_rules (service_id, start_time, end_time, sunday, monday, tuesday, wednesday, thursday, friday, saturday)
	select {{uuid "sched"}},
		(now() at time zone 'UTC' + interval '2 hours')::time,
		(now() at time zone 'UTC' + interval '3 hours')::time,
		true, true, true, true, true, true, true;
`

// suppressedWindowOpensIn is how far these tests fast-forward to move the
// scheduled service inside the window emptyFirstStepEPSQL builds for it --
// past its start, well before its end.
const suppressedWindowOpensIn = 2*time.Hour + 10*time.Minute

// alertIDs runs an alert search as the given user. input is the body of the
// search input object.
func alertIDs(t *testing.T, h *harness.Harness, userID, input string) []int {
	t.Helper()

	g := h.GraphQLQueryUserT(t, userID, fmt.Sprintf(`
		query { alerts(input: { %s }) { nodes { alertID } } }
	`, input))
	for _, err := range g.Errors {
		t.Fatal("GraphQL Error:", err.Message)
	}

	var res struct {
		Alerts struct {
			Nodes []struct{ AlertID int }
		}
	}
	require.NoError(t, json.Unmarshal(g.Data, &res))

	ids := make([]int, 0, len(res.Alerts.Nodes))
	for _, n := range res.Alerts.Nodes {
		ids = append(ids, n.AlertID)
	}
	return ids
}

// assignedAlertIDs returns the alerts the "assigned to me" filter reports for a
// user, which must match what each alert reports as its assignee.
func assignedAlertIDs(t *testing.T, h *harness.Harness, userID string) []int {
	t.Helper()
	return alertIDs(t, h, userID, fmt.Sprintf(`assignedUserID: "%s", first: 100`, userID))
}

// TestAlertAssignmentSuppressedFallsThrough covers alerts that never escalate.
//
// Suppressing notifications works by freezing the escalation policy, so a low
// urgency alert -- or one captured outside its service's alerting window --
// sits on step 0 for as long as it stays open. Off-hours that step routinely
// has nobody on-call, so ownership has to come from a later one.
func TestAlertAssignmentSuppressedFallsThrough(t *testing.T) {
	t.Parallel()

	h := harness.NewHarnessWithFlags(t, emptyFirstStepEPSQL, "add-service-alert-schedule", expflag.FlagSet{expflag.SvcAlertSchedule})
	defer h.Close()

	h.SetConfigValue("General.EnableAlertAssignment", "true")

	d1 := h.Twilio(t).Device(h.Phone("1"))

	low := h.CreateAlert(h.UUID("low"), "quiet")
	offHours := h.CreateAlert(h.UUID("sched"), "off hours")
	h.Trigger()

	for name, id := range map[string]int{"low urgency": low.ID(), "outside window": offHours.ID()} {
		res := queryAssignment(t, h, id)
		assert.Equal(t, "onCall", res.Alert.AssignmentSource, name)
		if assert.NotNil(t, res.Alert.AssignedUser, "%s: expected an assignee", name) {
			assert.Equal(t, h.UUID("user1"), res.Alert.AssignedUser.ID,
				"%s: ownership should fall through the empty first step", name)
		}
	}

	// Ownership is all that changed: the off-hours alert still waits for its
	// window before anyone is paged, and the low urgency one never pages at all.
	h.FastForward(suppressedWindowOpensIn)
	h.Trigger()
	d1.ExpectSMS("off hours")

	res := queryAssignment(t, h, low.ID())
	assert.Equal(t, "onCall", res.Alert.AssignmentSource)
	require.NotNil(t, res.Alert.AssignedUser)
	assert.Equal(t, h.UUID("user1"), res.Alert.AssignedUser.ID)
}

// TestAlertAssignmentSuppressedFilter covers the search side of the same rule:
// the alert list and the resolved assignee must agree, or an alert shows an
// owner it cannot be filtered by.
func TestAlertAssignmentSuppressedFilter(t *testing.T) {
	t.Parallel()

	h := harness.NewHarnessWithFlags(t, emptyFirstStepEPSQL, "add-service-alert-schedule", expflag.FlagSet{expflag.SvcAlertSchedule})
	defer h.Close()

	h.SetConfigValue("General.EnableAlertAssignment", "true")

	low := h.CreateAlert(h.UUID("low"), "quiet")
	offHours := h.CreateAlert(h.UUID("sched"), "off hours")
	h.Trigger()

	assert.ElementsMatch(t, []int{low.ID(), offHours.ID()}, assignedAlertIDs(t, h, h.UUID("user1")),
		"alerts that cannot escalate should still be filterable by their on-call owner")
}
