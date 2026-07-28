package smoke

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

	query := func(userID string) []int {
		t.Helper()
		g := h.GraphQLQueryUserT(t, userID, fmt.Sprintf(`
			query {
				alerts(input: { assignedUserID: "%s", first: 100 }) {
					nodes { alertID }
				}
			}
		`, userID))
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

	assert.ElementsMatch(t, []int{derived.ID()}, query(h.UUID("user1")),
		"user1 should see only the alert derived from being on-call")
	assert.ElementsMatch(t, []int{claimed.ID()}, query(h.UUID("user2")),
		"user2 should see only the alert they were assigned")
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
