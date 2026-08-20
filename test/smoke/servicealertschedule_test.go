package smoke

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/target/goalert/expflag"
	"github.com/target/goalert/test/smoke/harness"
)

// Every rule below enables all seven days, so nothing depends on which weekday
// the test happens to run on -- only on the time of day, which the tests
// control. Deriving the weekday from now() at seed time would invert these
// assertions on a run that straddles UTC midnight.
const alertScheduleInitSQL = `
insert into users (id, name, email)
values ({{uuid "user"}}, 'bob', 'bobby@domain.com');

insert into user_contact_methods (id, user_id, name, type, value)
values ({{uuid "cm1"}}, {{uuid "user"}}, 'personal', 'SMS', {{phone "1"}});

insert into user_notification_rules (user_id, contact_method_id, delay_minutes)
values ({{uuid "user"}}, {{uuid "cm1"}}, 0);

insert into escalation_policies (id, name)
values ({{uuid "eid"}}, 'esc policy');

insert into escalation_policy_steps (id, escalation_policy_id, delay)
values ({{uuid "es1"}}, {{uuid "eid"}}, 60);

insert into escalation_policy_actions (escalation_policy_step_id, user_id)
values ({{uuid "es1"}}, {{uuid "user"}});

insert into services (id, escalation_policy_id, name, alert_schedule_enabled, notification_time_zone)
values
	({{uuid "open"}}, {{uuid "eid"}}, 'open service', true, 'UTC'),
	({{uuid "closed"}}, {{uuid "eid"}}, 'closed service', true, 'UTC');

-- start = end means a full 24 hours, so this service is always inside its window.
insert into service_notification_rules (service_id, start_time, end_time, sunday, monday, tuesday, wednesday, thursday, friday, saturday)
values ({{uuid "open"}}, '00:00:00', '00:00:00', true, true, true, true, true, true, true);

-- A one-hour window starting an hour from now: closed at seed time, open after
-- a short fast-forward, on whatever day that turns out to be.
insert into service_notification_rules (service_id, start_time, end_time, sunday, monday, tuesday, wednesday, thursday, friday, saturday)
select
	{{uuid "closed"}},
	(now() at time zone 'UTC' + interval '1 hour')::time,
	(now() at time zone 'UTC' + interval '2 hours')::time,
	true, true, true, true, true, true, true;
`

// windowOpensIn is how far the tests fast-forward to move the 'closed' service
// inside its window -- comfortably past its start, well before its end.
const windowOpensIn = 70 * time.Minute

// gqlServiceInitSQL is a plain service with no alerting schedule; the tests
// below configure one through the API.
const gqlServiceInitSQL = `
insert into users (id, name, email)
values ({{uuid "user"}}, 'bob', 'bobby@domain.com');

insert into user_contact_methods (id, user_id, name, type, value)
values ({{uuid "cm1"}}, {{uuid "user"}}, 'personal', 'SMS', {{phone "1"}});

insert into user_notification_rules (user_id, contact_method_id, delay_minutes)
values ({{uuid "user"}}, {{uuid "cm1"}}, 0);

insert into escalation_policies (id, name)
values ({{uuid "eid"}}, 'esc policy');

insert into escalation_policy_steps (id, escalation_policy_id, delay)
values ({{uuid "es1"}}, {{uuid "eid"}}, 60);

insert into escalation_policy_actions (escalation_policy_step_id, user_id)
values ({{uuid "es1"}}, {{uuid "user"}});

insert into services (id, escalation_policy_id, name)
values ({{uuid "sid"}}, {{uuid "eid"}}, 'service');`

func setScheduleMutation(serviceID, start, end string) string {
	return fmt.Sprintf(`
	mutation {
		updateService(input: {
			id: "%s",
			alertScheduleEnabled: true,
			notificationTimeZone: "UTC",
			notificationRules: [{
				start: "%s",
				end: "%s",
				weekdayFilter: [true, true, true, true, true, true, true]
			}]
		})
	}`, serviceID, start, end)
}

// closedWindow returns a one-minute window, every day, that ended two hours
// ago -- so the service is closed right now whenever the test runs.
func closedWindow(h *harness.Harness) (start, end string) {
	at := h.Now().UTC().Add(-2 * time.Hour)
	return at.Format("15:04"), at.Add(time.Minute).Format("15:04")
}

// TestServiceAlertScheduleGraphQL configures the windows through the API rather
// than seeded SQL, and confirms an alert captured while closed notifies once
// the window is widened.
func TestServiceAlertScheduleGraphQL(t *testing.T) {
	t.Parallel()

	h := harness.NewHarnessWithFlags(t, gqlServiceInitSQL, "add-service-alert-schedule", expflag.FlagSet{expflag.SvcAlertSchedule})
	defer h.Close()

	start, end := closedWindow(h)
	require.Empty(t, h.GraphQLQuery2(setScheduleMutation(h.UUID("sid"), start, end)).Errors)

	h.CreateAlert(h.UUID("sid"), "quiet hours")
	h.Trigger()

	// Widen the window to the whole day (start == end is 24h); the captured
	// alert now escalates.
	d := h.Twilio(t).Device(h.Phone("1"))
	require.Empty(t, h.GraphQLQuery2(setScheduleMutation(h.UUID("sid"), "00:00", "00:00")).Errors)
	h.Trigger()

	d.ExpectSMS("quiet hours")
}

// TestServiceAlertScheduleFlagGate confirms the feature is inert without the
// experimental flag.
func TestServiceAlertScheduleFlagGate(t *testing.T) {
	t.Parallel()

	h := harness.NewHarness(t, gqlServiceInitSQL, "add-service-alert-schedule")
	defer h.Close()

	res := h.GraphQLQuery2(setScheduleMutation(h.UUID("sid"), "09:00", "17:00"))
	require.NotEmpty(t, res.Errors, "enabling alerting windows without the flag should be rejected")

	// The service still notifies as normal.
	d := h.Twilio(t).Device(h.Phone("1"))
	h.CreateAlert(h.UUID("sid"), "unchanged")
	d.ExpectSMS("unchanged")
}

// TestServiceAlertScheduleEditKeepsWindowOpen pins the behavior that an edit
// unrelated to alerting does not re-suppress a service that is currently inside
// its window.
//
// Recomputing suppression on every write would pause paging until the next
// engine pass -- and refuse manual escalation in the meantime -- for something
// as innocuous as a rename. The check deliberately runs with no engine cycle in
// between, because the engine would otherwise heal the flag and hide the bug.
func TestServiceAlertScheduleEditKeepsWindowOpen(t *testing.T) {
	t.Parallel()

	h := harness.NewHarnessWithFlags(t, alertScheduleInitSQL, "add-service-alert-schedule", expflag.FlagSet{expflag.SvcAlertSchedule})
	defer h.Close()

	// Let the engine observe that the window is open.
	h.Trigger()
	require.False(t, alertScheduleSuppressed(t, h, h.UUID("open")), "service should start open")

	rename := fmt.Sprintf(`mutation { updateService(input: {id: "%s", name: "renamed service"}) }`, h.UUID("open"))
	require.Empty(t, h.GraphQLQuery2(rename).Errors)

	require.False(t, alertScheduleSuppressed(t, h, h.UUID("open")),
		"an unrelated edit must not suppress a service inside its window")
}

// TestServiceAlertScheduleEditableWithoutFlag covers the rollback path: once the
// experimental flag is off, services already using schedule-based alerting must
// stay editable, including turning the windows back off. Otherwise turning the
// flag off strands them.
func TestServiceAlertScheduleEditableWithoutFlag(t *testing.T) {
	t.Parallel()

	h := harness.NewHarness(t, alertScheduleInitSQL, "add-service-alert-schedule")
	defer h.Close()

	rename := fmt.Sprintf(`mutation { updateService(input: {id: "%s", name: "renamed service"}) }`, h.UUID("open"))
	require.Empty(t, h.GraphQLQuery2(rename).Errors, "renaming a schedule-enabled service should work without the flag")

	// And there is a way to turn alerting windows off again.
	disable := fmt.Sprintf(`mutation { updateService(input: {id: "%s", alertScheduleEnabled: false}) }`, h.UUID("open"))
	require.Empty(t, h.GraphQLQuery2(disable).Errors, "disabling alerting windows should work without the flag")

	d := h.Twilio(t).Device(h.Phone("1"))
	h.CreateAlert(h.UUID("open"), "windows off")
	d.ExpectSMS("windows off")
}

func alertScheduleSuppressed(t *testing.T, h *harness.Harness, serviceID string) bool {
	t.Helper()

	res := h.GraphQLQuery2(fmt.Sprintf(`query { service(id: "%s") { notificationSuppressed } }`, serviceID))
	require.Empty(t, res.Errors)

	var data struct {
		Service struct{ NotificationSuppressed bool }
	}
	require.NoError(t, json.Unmarshal(res.Data, &data))

	return data.Service.NotificationSuppressed
}
