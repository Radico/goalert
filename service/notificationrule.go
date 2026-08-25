package service

import (
	"time"

	"github.com/target/goalert/schedule/rule"
	"github.com/target/goalert/util/timeutil"
	"github.com/target/goalert/validation"
	"github.com/target/goalert/validation/validate"
)

// MaxNotificationRules is the number of alerting windows a single service may
// define.
const MaxNotificationRules = 24

// NotificationRule is a recurring weekly window during which a service with
// UrgencyScheduled will notify.
//
// It has the same shape as a schedule rule: a weekday filter plus a start and
// end time-of-day, interpreted in the service's NotificationTimeZone. Start
// after End means the window runs overnight and is attributed to the day it
// starts on; Start equal to End means a full 24 hours.
type NotificationRule struct {
	timeutil.WeekdayFilter
	Start timeutil.Clock `json:"start"`
	End   timeutil.Clock `json:"end"`
}

// WeekdayFilterFromDays builds a WeekdayFilter from the seven boolean columns
// the rules table stores. Both the service store and the escalation manager
// read those columns, so the mapping lives in one place -- getting the day
// order wrong would silently alert on the wrong days.
func WeekdayFilterFromDays(sun, mon, tue, wed, thu, fri, sat bool) timeutil.WeekdayFilter {
	var f timeutil.WeekdayFilter
	for i, day := range []bool{sun, mon, tue, wed, thu, fri, sat} {
		if day {
			f[i] = 1
		}
	}
	return f
}

// asScheduleRule adapts the rule so activity is decided by exactly one
// implementation -- the one the scheduling engine already uses, which is
// DST-correct and covered by schedule/rule's test suite. Rule.IsActive reads
// only the weekday filter and the clock values, so the zero ScheduleID and
// Target here are never consulted.
func (r NotificationRule) asScheduleRule() rule.Rule {
	return rule.Rule{
		WeekdayFilter: r.WeekdayFilter,
		Start:         r.Start,
		End:           r.End,
	}
}

// IsActive reports whether the window covers t, evaluated in the location of t.
// Callers are responsible for converting t into the service's timezone first.
func (r NotificationRule) IsActive(t time.Time) bool {
	return r.asScheduleRule().IsActive(t)
}

// String returns a human-readable description of the window, e.g. "9am-5pm M-F".
func (r NotificationRule) String() string { return r.asScheduleRule().String() }

// Normalize truncates the clock values to the minute and validates the rule.
func (r NotificationRule) Normalize() (*NotificationRule, error) {
	r.Start = timeutil.Clock(time.Duration(r.Start).Truncate(time.Minute))
	r.End = timeutil.Clock(time.Duration(r.End).Truncate(time.Minute))

	// A rule with no days enabled would silently never fire, which reads as a
	// misconfiguration rather than an intent to mute.
	if r.IsNever() {
		return nil, validation.NewFieldError("WeekdayFilter", "must have at least one day enabled")
	}

	return &r, nil
}

// normalizeNotificationRules validates the full rule set for a service.
func normalizeNotificationRules(rules []NotificationRule) ([]NotificationRule, error) {
	err := validate.Range("NotificationRules", len(rules), 1, MaxNotificationRules)
	if err != nil {
		return nil, err
	}

	result := make([]NotificationRule, 0, len(rules))
	for _, r := range rules {
		n, err := r.Normalize()
		if err != nil {
			return nil, err
		}
		result = append(result, *n)
	}

	return result, nil
}
