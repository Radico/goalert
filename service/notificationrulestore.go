package service

import (
	"context"

	"github.com/google/uuid"
	"github.com/target/goalert/gadb"
)

// loadNotificationRules fills in NotificationRules for any of the given
// services that use schedule-based alerting. Services without it have no rules
// by construction, so they are skipped rather than queried for.
func loadNotificationRules(ctx context.Context, q *gadb.Queries, svcs []Service) error {
	byID := make(map[uuid.UUID]*Service)
	for i := range svcs {
		if !svcs[i].AlertScheduleEnabled {
			continue
		}
		id, err := uuid.Parse(svcs[i].ID)
		if err != nil {
			return err
		}
		byID[id] = &svcs[i]
	}
	if len(byID) == 0 {
		return nil
	}

	ids := make([]uuid.UUID, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}

	rows, err := q.ServiceNotificationRules_FindMany(ctx, ids)
	if err != nil {
		return err
	}

	for _, row := range rows {
		svc, ok := byID[row.ServiceID]
		if !ok {
			continue
		}
		svc.NotificationRules = append(svc.NotificationRules, NotificationRule{
			WeekdayFilter: WeekdayFilterFromDays(row.Sunday, row.Monday, row.Tuesday, row.Wednesday, row.Thursday, row.Friday, row.Saturday),
			Start:         row.StartTime,
			End:           row.EndTime,
		})
	}

	return nil
}

// replaceNotificationRules makes the stored rule set match svc exactly.
// Normalize clears the rules when schedule-based alerting is off, so this also
// cleans up after a service that turns it off.
func replaceNotificationRules(ctx context.Context, q *gadb.Queries, svc *Service) error {
	id, err := uuid.Parse(svc.ID)
	if err != nil {
		return err
	}

	err = q.ServiceNotificationRules_DeleteByService(ctx, id)
	if err != nil {
		return err
	}

	for _, r := range svc.NotificationRules {
		err = q.ServiceNotificationRules_Insert(ctx, gadb.ServiceNotificationRules_InsertParams{
			ServiceID: id,
			StartTime: r.Start,
			EndTime:   r.End,
			Sunday:    r.Day(0),
			Monday:    r.Day(1),
			Tuesday:   r.Day(2),
			Wednesday: r.Day(3),
			Thursday:  r.Day(4),
			Friday:    r.Day(5),
			Saturday:  r.Day(6),
		})
		if err != nil {
			return err
		}
	}

	return nil
}
