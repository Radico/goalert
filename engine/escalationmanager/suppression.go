package escalationmanager

import (
	"context"
	"database/sql"

	"github.com/google/uuid"
	"github.com/target/goalert/gadb"
	"github.com/target/goalert/service"
	"github.com/target/goalert/util"
	"github.com/target/goalert/util/log"
)

// updateSuppressedServices recomputes services.notification_suppressed for
// every service using schedule-based alerting.
//
// The weekly-window predicate is evaluated here, in Go, rather than in the
// escalation queries themselves. That keeps a single implementation of it --
// schedule/rule.Rule.IsActive, which is DST-correct where a SQL
// `(now() AT TIME ZONE tz)::time` comparison is not -- and it isolates a
// service with an unusable timezone to itself instead of aborting the whole
// escalation batch.
//
// The resulting flag is tick-consistent, not instantaneously consistent: it is
// committed before the escalation queries run in the same pass, but a window
// boundary can still fall between them. That is the same contract every other
// materialized-state module in the engine operates under.
func (db *DB) updateSuppressedServices(ctx context.Context) error {
	return db.lock.WithTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		return db.updateSuppressedServicesTx(ctx, tx)
	})
}

func (db *DB) updateSuppressedServicesTx(ctx context.Context, tx *sql.Tx) error {
	q := gadb.New(tx)

	rows, err := q.EscMgrScheduledServices(ctx)
	if err != nil {
		return err
	}

	type svcRules struct {
		tz    string
		rules []service.NotificationRule
	}

	byService := make(map[uuid.UUID]*svcRules, len(rows))
	for _, row := range rows {
		s, ok := byService[row.ServiceID]
		if !ok {
			s = &svcRules{tz: row.NotificationTimeZone.String}
			byService[row.ServiceID] = s
		}

		s.rules = append(s.rules, service.NotificationRule{
			WeekdayFilter: service.WeekdayFilterFromDays(row.Sunday, row.Monday, row.Tuesday, row.Wednesday, row.Thursday, row.Friday, row.Saturday),
			Start:         row.StartTime,
			End:           row.EndTime,
		})
	}

	// Use the database clock, not the process clock: every other time-dependent
	// decision in the engine is made with now(), and the two can differ.
	now, err := q.Now(ctx)
	if err != nil {
		return err
	}

	open := make([]uuid.UUID, 0, len(byService))
	for id, s := range byService {
		loc, err := util.LoadLocation(s.tz)
		if err != nil {
			// Fail closed: leave the service suppressed and keep going, so one
			// bad row cannot stop escalation for everyone else.
			log.Log(log.WithField(ctx, "ServiceID", id.String()), err)
			continue
		}

		local := now.In(loc)
		for _, r := range s.rules {
			if r.IsActive(local) {
				open = append(open, id)
				break
			}
		}
	}

	return q.EscMgrSetSuppressedServices(ctx, open)
}
