package alert

import "fmt"

// AssignmentSource indicates where an alert's assignee came from.
type AssignmentSource string

const (
	// AssignmentSourceUnassigned means the alert has no explicit assignee and
	// nobody is currently on-call for its escalation step.
	AssignmentSourceUnassigned AssignmentSource = "unassigned"

	// AssignmentSourceOnCall means the alert has not been claimed, so the
	// assignee is derived from whoever is on-call for the alert's current
	// escalation step. This value changes as the alert escalates or as
	// rotations hand off.
	AssignmentSourceOnCall AssignmentSource = "onCall"

	// AssignmentSourceExplicit means a user has claimed the alert (by
	// acknowledging it) or it was explicitly re-assigned. Explicit assignments
	// are frozen: they never change on escalation or rotation handoff.
	AssignmentSourceExplicit AssignmentSource = "explicit"
)

// An Assignment is the resolved ownership of an alert.
//
// Assignment is triage metadata only -- it never affects notification routing,
// which remains entirely determined by the service's escalation policy.
type Assignment struct {
	AlertID int
	UserID  string
	Source  AssignmentSource
}

func (s *AssignmentSource) Scan(value interface{}) error {
	switch t := value.(type) {
	case []byte:
		*s = AssignmentSource(t)
	case string:
		*s = AssignmentSource(t)
	case nil:
		*s = AssignmentSourceUnassigned
	default:
		return fmt.Errorf("could not process unknown type for assignment source %T", t)
	}
	return nil
}
