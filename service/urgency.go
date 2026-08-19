package service

import "fmt"

// Urgency controls whether alerts for a service notify anyone.
type Urgency string

const (
	// UrgencyHigh is the default. Alerts follow the escalation policy and
	// notify on-call users as normal.
	UrgencyHigh Urgency = "high"

	// UrgencyLow records alerts for tracking only. They are created and appear
	// in the UI, but the escalation policy is never run for them, so no
	// notifications are sent.
	UrgencyLow Urgency = "low"

	// UrgencyScheduled notifies only during the service's configured weekly
	// alerting windows (see NotificationRule). Outside those windows it behaves
	// exactly like UrgencyLow: alerts are recorded, but the escalation policy is
	// not run. An alert captured outside a window escalates when the window next
	// opens, because its escalation policy never started.
	UrgencyScheduled Urgency = "scheduled"
)

// Scan handles reading Urgency from the DB format.
func (u *Urgency) Scan(value interface{}) error {
	switch t := value.(type) {
	case []byte:
		*u = Urgency(t)
	case string:
		*u = Urgency(t)
	case nil:
		// pre-migration rows, and any NULL, mean the default
		*u = UrgencyHigh
	default:
		return fmt.Errorf("could not process unknown type for urgency %T", t)
	}

	return nil
}
