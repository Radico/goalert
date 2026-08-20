package service

import (
	"testing"
	"time"

	"github.com/target/goalert/util/timeutil"
)

const testTimeFmt = "Mon Jan _2 3:04PM 2006 MST"

func newRule(start, end timeutil.Clock, days ...time.Weekday) NotificationRule {
	r := NotificationRule{Start: start, End: end}
	for _, d := range days {
		r.SetDay(d, true)
	}
	return r
}

func mustLoad(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("load location %s: %v", name, err)
	}
	return loc
}

// TestNotificationRule_IsActive covers the weekday indexing and window shapes.
// The underlying clock math is schedule/rule's; what is worth pinning here is
// that this type wires into it correctly -- an off-by-one in the weekday filter
// would silently alert on the wrong days.
func TestNotificationRule_IsActive(t *testing.T) {
	// A 24x5 window, the motivating case: staffed all week, quiet at the weekend.
	weekdays := newRule(timeutil.NewClock(0, 0), timeutil.NewClock(0, 0),
		time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday)

	// A business-hours window.
	business := newRule(timeutil.NewClock(9, 0), timeutil.NewClock(17, 0),
		time.Monday, time.Wednesday)

	// An overnight window, attributed to the day it starts on.
	overnight := newRule(timeutil.NewClock(20, 0), timeutil.NewClock(6, 0), time.Friday)

	// Every day, all day.
	always := newRule(timeutil.NewClock(0, 0), timeutil.NewClock(0, 0),
		time.Sunday, time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday, time.Saturday)

	utc := time.UTC

	check := []struct {
		name string
		rule NotificationRule
		at   time.Time
		want bool
	}{
		{"24x5 Wed noon", weekdays, time.Date(2024, 5, 8, 12, 0, 0, 0, utc), true},
		{"24x5 Wed 3am", weekdays, time.Date(2024, 5, 8, 3, 0, 0, 0, utc), true},
		{"24x5 Sat noon", weekdays, time.Date(2024, 5, 11, 12, 0, 0, 0, utc), false},
		{"24x5 Sun noon", weekdays, time.Date(2024, 5, 12, 12, 0, 0, 0, utc), false},
		{"24x5 Mon 00:00 exactly", weekdays, time.Date(2024, 5, 6, 0, 0, 0, 0, utc), true},

		{"business Mon 9am exactly", business, time.Date(2024, 5, 6, 9, 0, 0, 0, utc), true},
		{"business Mon 8:59am", business, time.Date(2024, 5, 6, 8, 59, 0, 0, utc), false},
		{"business Mon 4:59pm", business, time.Date(2024, 5, 6, 16, 59, 0, 0, utc), true},
		{"business Mon 5:01pm", business, time.Date(2024, 5, 6, 17, 1, 0, 0, utc), false},
		{"business Tue noon (day off)", business, time.Date(2024, 5, 7, 12, 0, 0, 0, utc), false},
		{"business Wed noon", business, time.Date(2024, 5, 8, 12, 0, 0, 0, utc), true},

		{"overnight Fri 9pm", overnight, time.Date(2024, 5, 10, 21, 0, 0, 0, utc), true},
		{"overnight Fri 7pm", overnight, time.Date(2024, 5, 10, 19, 0, 0, 0, utc), false},
		{"overnight Sat 2am (spills over)", overnight, time.Date(2024, 5, 11, 2, 0, 0, 0, utc), true},
		{"overnight Sat 7am", overnight, time.Date(2024, 5, 11, 7, 0, 0, 0, utc), false},
		{"overnight Sat 9pm (Sat not enabled)", overnight, time.Date(2024, 5, 11, 21, 0, 0, 0, utc), false},

		{"always", always, time.Date(2024, 5, 11, 3, 0, 0, 0, utc), true},
	}

	for _, c := range check {
		t.Run(c.name+"/"+c.at.Format(testTimeFmt), func(t *testing.T) {
			if got := c.rule.IsActive(c.at); got != c.want {
				t.Errorf("IsActive = %t; want %t", got, c.want)
			}
		})
	}
}

// TestNotificationRule_IsActive_DST pins behavior across US daylight saving
// transitions. Windows are wall-clock: 9-to-5 stays 9-to-5 on both sides.
func TestNotificationRule_IsActive_DST(t *testing.T) {
	chi := mustLoad(t, "America/Chicago")

	// 2024-03-10: 2am jumps to 3am. 2024-11-03: 2am repeats 1am.
	business := newRule(timeutil.NewClock(9, 0), timeutil.NewClock(17, 0), time.Sunday)
	overnight := newRule(timeutil.NewClock(20, 0), timeutil.NewClock(6, 0), time.Saturday)

	check := []struct {
		name string
		rule NotificationRule
		at   time.Time
		want bool
	}{
		{"spring-forward before window", business, time.Date(2024, 3, 10, 8, 0, 0, 0, chi), false},
		{"spring-forward inside window", business, time.Date(2024, 3, 10, 12, 0, 0, 0, chi), true},
		{"spring-forward after window", business, time.Date(2024, 3, 10, 18, 0, 0, 0, chi), false},
		// Overnight window spanning the lost hour: 8pm Sat through 6am Sun.
		{"overnight across spring-forward", overnight, time.Date(2024, 3, 10, 4, 0, 0, 0, chi), true},

		{"fall-back inside window", business, time.Date(2024, 11, 3, 12, 0, 0, 0, chi), true},
		// Overnight window spanning the repeated hour: still active at 1:30am,
		// whichever of the two 1:30s it is.
		{"overnight across fall-back", overnight, time.Date(2024, 11, 3, 1, 30, 0, 0, chi), true},
		{"overnight after fall-back end", overnight, time.Date(2024, 11, 3, 7, 0, 0, 0, chi), false},
	}

	for _, c := range check {
		t.Run(c.name+"/"+c.at.Format(testTimeFmt), func(t *testing.T) {
			if got := c.rule.IsActive(c.at); got != c.want {
				t.Errorf("IsActive = %t; want %t", got, c.want)
			}
		})
	}
}

// TestNotificationRule_IsActive_HalfHourZone guards against assuming whole-hour
// UTC offsets.
func TestNotificationRule_IsActive_HalfHourZone(t *testing.T) {
	kol := mustLoad(t, "Asia/Kolkata") // UTC+5:30

	r := newRule(timeutil.NewClock(9, 0), timeutil.NewClock(17, 0), time.Monday)

	// 2024-05-06 05:00 UTC is 10:30 Monday in Kolkata -- inside the window.
	at := time.Date(2024, 5, 6, 5, 0, 0, 0, time.UTC).In(kol)
	if !r.IsActive(at) {
		t.Errorf("IsActive(%s) = false; want true", at.Format(testTimeFmt))
	}

	// 2024-05-06 03:00 UTC is 08:30 Monday -- before it opens.
	at = time.Date(2024, 5, 6, 3, 0, 0, 0, time.UTC).In(kol)
	if r.IsActive(at) {
		t.Errorf("IsActive(%s) = true; want false", at.Format(testTimeFmt))
	}
}

func TestNotificationRule_Normalize(t *testing.T) {
	t.Run("rejects a rule with no days", func(t *testing.T) {
		r := NotificationRule{Start: timeutil.NewClock(9, 0), End: timeutil.NewClock(17, 0)}
		if _, err := r.Normalize(); err == nil {
			t.Error("expected an error for a rule with no enabled days")
		}
	})

	t.Run("truncates to the minute", func(t *testing.T) {
		r := newRule(timeutil.Clock(9*time.Hour+30*time.Second), timeutil.NewClock(17, 0), time.Monday)
		n, err := r.Normalize()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if want := timeutil.NewClock(9, 0); n.Start != want {
			t.Errorf("Start = %s; want %s", n.Start, want)
		}
	})
}

func TestNormalizeNotificationRules(t *testing.T) {
	valid := newRule(timeutil.NewClock(9, 0), timeutil.NewClock(17, 0), time.Monday)

	if _, err := normalizeNotificationRules(nil); err == nil {
		t.Error("expected an error for an empty rule set")
	}

	tooMany := make([]NotificationRule, MaxNotificationRules+1)
	for i := range tooMany {
		tooMany[i] = valid
	}
	if _, err := normalizeNotificationRules(tooMany); err == nil {
		t.Errorf("expected an error for more than %d rules", MaxNotificationRules)
	}

	if _, err := normalizeNotificationRules([]NotificationRule{valid}); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}
