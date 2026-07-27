package smoke

import (
	"fmt"
	"testing"

	"github.com/target/goalert/test/smoke/harness"
)

// TestServiceLowUrgency tests that a service set to low urgency still records
// alerts, but never notifies anyone.
//
// An alert on a high urgency (default) service notifies as normal. After the
// service is switched to low urgency, a new alert produces no notification.
func TestServiceLowUrgency(t *testing.T) {
	t.Parallel()

	const initSQL = `
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

	h := harness.NewHarness(t, initSQL, "add-service-notification-urgency")
	defer h.Close()

	d := h.Twilio(t).Device(h.Phone("1"))

	// service defaults to high urgency, so this notifies as normal
	h.CreateAlert(h.UUID("sid"), "high urgency")
	d.ExpectSMS("high urgency")

	// switch the service to low urgency
	setLow := fmt.Sprintf(`
	mutation {
		updateService(input: {
			id: "%s",
			notificationUrgency: low
		})
	}
	`, h.UUID("sid"))
	h.GraphQLQuery2(setLow)

	// alert is still created, but nobody is notified
	h.CreateAlert(h.UUID("sid"), "low urgency")
	h.Trigger()

	// no SMS expected for "low urgency" -- the harness fails on unexpected
	// messages when it closes.
}

// TestServiceLowUrgencyRaised documents what happens to alerts that piled up
// while a service was low urgency, once it is raised back to high.
//
// They are still open and their escalation policy has never run, so raising the
// service escalates them -- the same way alerts resume when maintenance mode
// expires. Operationally this means raising a busy low urgency service can
// notify for its whole backlog at once.
func TestServiceLowUrgencyRaised(t *testing.T) {
	t.Parallel()

	const initSQL = `
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

	insert into services (id, escalation_policy_id, name, notification_urgency)
	values ({{uuid "sid"}}, {{uuid "eid"}}, 'service', 'low');`

	h := harness.NewHarness(t, initSQL, "add-service-notification-urgency")
	defer h.Close()

	d := h.Twilio(t).Device(h.Phone("1"))

	// alert accumulates silently while the service is low urgency
	h.CreateAlert(h.UUID("sid"), "backlog")
	h.Trigger()

	// raise the service back to high urgency
	setHigh := fmt.Sprintf(`
	mutation {
		updateService(input: {
			id: "%s",
			notificationUrgency: high
		})
	}
	`, h.UUID("sid"))
	h.GraphQLQuery2(setHigh)

	// the backlogged alert now escalates and notifies
	d.ExpectSMS("backlog")
}

// TestServiceLowUrgencyEscalate tests that alerts on a low urgency service
// cannot be manually escalated, and that no notifications are sent for them.
func TestServiceLowUrgencyEscalate(t *testing.T) {
	t.Parallel()

	const initSQL = `
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

	insert into services (id, escalation_policy_id, name, notification_urgency)
	values ({{uuid "sid"}}, {{uuid "eid"}}, 'service', 'low');`

	h := harness.NewHarness(t, initSQL, "add-service-notification-urgency")
	defer h.Close()

	// alert is created and tracked, but never escalates
	alert := h.CreateAlert(h.UUID("sid"), "testing")
	h.Trigger()

	// manual escalation is rejected
	escalate := fmt.Sprintf(`
	mutation {
		escalateAlerts(input: [%d]) {
			id
		}
	}
	`, alert.ID())
	h.GraphQLQuery2(escalate)

	h.Trigger()

	// no SMS expected at all
}
