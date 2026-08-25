package service

import (
	"time"

	"github.com/target/goalert/util"
	"github.com/target/goalert/validation"
	"github.com/target/goalert/validation/validate"
)

type Service struct {
	ID                   string
	Name                 string
	Description          string
	EscalationPolicyID   string
	MaintenanceExpiresAt time.Time
	NotificationUrgency  Urgency

	// NotificationTimeZone and NotificationRules define the weekly alerting
	// windows used when NotificationUrgency is UrgencyScheduled. They are
	// cleared for every other urgency, so an unrelated edit cannot leave stale
	// windows behind.
	NotificationTimeZone string
	NotificationRules    []NotificationRule

	// NotificationSuppressed is materialized by the escalation manager: true
	// while the current time falls outside every NotificationRule. Read-only
	// from the API's point of view.
	NotificationSuppressed bool

	epName         string
	isUserFavorite bool
}

const MaxDetailsLength = 6 * 1024 // 6KiB

func (s Service) EscalationPolicyName() string {
	return s.epName
}

// IsUserFavorite returns a boolean value based on if the service is a favorite of the user or not.
func (s Service) IsUserFavorite() bool {
	return s.isUserFavorite
}

// Normalize will validate and 'normalize' the Service -- such as setting the minimum duration to 0.
func (s Service) Normalize() (*Service, error) {
	dur := time.Until(s.MaintenanceExpiresAt)

	if dur <= 0 {
		dur = 0
		s.MaintenanceExpiresAt = time.Time{}
	}

	// An empty value means the service predates urgency, or the caller didn't
	// set it -- either way it is high urgency.
	if s.NotificationUrgency == "" {
		s.NotificationUrgency = UrgencyHigh
	}

	err := validate.Many(
		validate.IDName("Name", s.Name),
		validate.Text("Description", s.Description, 1, MaxDetailsLength),
		validate.UUID("EscalationPolicyID", s.EscalationPolicyID),
		validate.Duration("MaintenanceExpiresAt", dur, 0, 24*time.Hour+5*time.Minute),
		validate.OneOf("NotificationUrgency", s.NotificationUrgency, UrgencyHigh, UrgencyLow, UrgencyScheduled),
	)
	if err != nil {
		return nil, err
	}

	if s.NotificationUrgency == UrgencyScheduled {
		// The zone is what turns the rules' wall-clock times into instants, and
		// the engine loads it on every pass -- reject anything Go cannot resolve
		// rather than discovering it later.
		if _, err := util.LoadLocation(s.NotificationTimeZone); err != nil {
			return nil, validation.NewFieldError("NotificationTimeZone", "must be a valid IANA time zone")
		}

		rules, err := normalizeNotificationRules(s.NotificationRules)
		if err != nil {
			return nil, err
		}
		s.NotificationRules = rules
	} else {
		s.NotificationTimeZone = ""
		s.NotificationRules = nil
	}

	return &s, nil
}
