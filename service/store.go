package service

import (
	"context"
	"database/sql"

	"github.com/target/goalert/gadb"
	"github.com/target/goalert/permission"
	"github.com/target/goalert/util"
	"github.com/target/goalert/util/sqlutil"
	"github.com/target/goalert/validation/validate"

	"github.com/google/uuid"
)

type Store struct {
	db *sql.DB

	findOne     *sql.Stmt
	findOneUp   *sql.Stmt
	findMany    *sql.Stmt
	findAllByEP *sql.Stmt
	insert      *sql.Stmt
	update      *sql.Stmt
	delete      *sql.Stmt
}

func NewStore(ctx context.Context, db *sql.DB) (*Store, error) {
	prep := &util.Prepare{DB: db, Ctx: ctx}
	p := prep.P

	s := &Store{db: db}
	s.findOne = p(`
		SELECT
			s.id,
			s.name,
			s.description,
			s.escalation_policy_id,
			e.name,
			fav	is distinct from null,
			s.maintenance_expires_at,
			s.alert_schedule_enabled,
			s.notification_time_zone,
			s.notification_suppressed
		FROM
			services s
		JOIN escalation_policies e ON e.id = s.escalation_policy_id
		LEFT JOIN user_favorites fav ON s.id = fav.tgt_service_id AND fav.user_id = $2
		WHERE
			s.id = $1
	`)
	s.findOneUp = p(`
		SELECT
			s.id,
			s.name,
			s.description,
			s.escalation_policy_id,
			s.alert_schedule_enabled,
			s.notification_time_zone,
			s.notification_suppressed
		FROM services s
		WHERE s.id = $1
		FOR UPDATE
	`)
	s.findMany = p(`
		SELECT
			s.id,
			s.name,
			s.description,
			s.escalation_policy_id,
			e.name,
			fav	is distinct from null,
			s.maintenance_expires_at,
			s.alert_schedule_enabled,
			s.notification_time_zone,
			s.notification_suppressed
		FROM
			services s
		JOIN escalation_policies e ON e.id = s.escalation_policy_id
		LEFT JOIN user_favorites fav ON s.id = fav.tgt_service_id AND fav.user_id = $2
		WHERE
			s.id = any($1)
	`)

	s.findAllByEP = p(`
		SELECT
			s.id,
			s.name,
			s.description,
			s.escalation_policy_id,
			e.name,
			false,
			s.maintenance_expires_at,
			s.alert_schedule_enabled,
			s.notification_time_zone,
			s.notification_suppressed
		FROM
			services s,
			escalation_policies e
		WHERE
			e.id = $1 AND
			e.id = s.escalation_policy_id
	`)
	// A service created with alerting windows starts suppressed, and the
	// escalation manager opens it on its next pass if a window is in fact open.
	// Starting suppressed means a misconfigured window can only delay paging by
	// a tick, never page outside its window.
	s.insert = p(`INSERT INTO services (id,name,description,escalation_policy_id,alert_schedule_enabled,notification_time_zone,notification_suppressed) VALUES ($1,$2,$3,$4,$5,$6,$5)`)
	// Suppression is only forced on when a service *enables* alerting windows.
	// Recomputing it on every update would re-suppress a service that is
	// currently inside its window, pausing paging until the next engine tick --
	// indefinitely if the engine is stalled -- for something as unrelated as a
	// rename or cancelling maintenance mode.
	s.update = p(`
		UPDATE services SET
			name = $2,
			description = $3,
			escalation_policy_id = $4,
			maintenance_expires_at = $5,
			alert_schedule_enabled = $6,
			notification_time_zone = $7,
			notification_suppressed = ($6 AND (NOT alert_schedule_enabled OR notification_suppressed))
		WHERE id = $1
	`)
	s.delete = p(`DELETE FROM services WHERE id = any($1)`)

	return s, prep.Err
}

func (s *Store) FindOneForUpdate(ctx context.Context, tx *sql.Tx, id string) (*Service, error) {
	err := permission.LimitCheckAny(ctx, permission.User)
	if err != nil {
		return nil, err
	}
	err = validate.UUID("ServiceID", id)
	if err != nil {
		return nil, err
	}
	var svc Service
	var tz sql.NullString
	err = tx.StmtContext(ctx, s.findOneUp).QueryRowContext(ctx, id).Scan(&svc.ID, &svc.Name, &svc.Description, &svc.EscalationPolicyID, &svc.AlertScheduleEnabled, &tz, &svc.NotificationSuppressed)
	if err != nil {
		return nil, err
	}
	svc.NotificationTimeZone = tz.String

	// loadNotificationRules fills in through the slice, so read the result back
	// out of it rather than from the copy that went in.
	one := []Service{svc}
	err = loadNotificationRules(ctx, gadb.New(tx), one)
	if err != nil {
		return nil, err
	}

	return &one[0], nil
}

// FindMany returns slice of Service objects given a slice of serviceIDs
func (s *Store) FindMany(ctx context.Context, ids []string) ([]Service, error) {
	err := permission.LimitCheckAny(ctx, permission.User)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}
	err = validate.ManyUUID("ServiceIDs", ids, 100)
	if err != nil {
		return nil, err
	}

	rows, err := s.findMany.QueryContext(ctx, sqlutil.UUIDArray(ids), permission.UserNullUUID(ctx))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return s.scanAllWithRules(ctx, rows)
}

func (s *Store) CreateServiceTx(ctx context.Context, tx *sql.Tx, svc *Service) (*Service, error) {
	err := permission.LimitCheckAny(ctx, permission.Admin, permission.User)
	if err != nil {
		return nil, err
	}

	// The service row and its notification rules are two statements, so they
	// need a transaction between them: a service left with alerting windows
	// enabled but no rules is permanently suppressed.
	tx, commit, release, err := s.ensureTx(ctx, tx)
	if err != nil {
		return nil, err
	}
	defer release()

	n, err := svc.Normalize()
	if err != nil {
		return nil, err
	}

	n.ID = uuid.New().String()
	_, err = tx.StmtContext(ctx, s.insert).ExecContext(ctx, n.ID, n.Name, n.Description, n.EscalationPolicyID, n.AlertScheduleEnabled, newNullString(n.NotificationTimeZone))
	if err != nil {
		return nil, err
	}
	n.NotificationSuppressed = n.AlertScheduleEnabled

	err = replaceNotificationRules(ctx, gadb.New(tx), n)
	if err != nil {
		return nil, err
	}

	err = commit()
	if err != nil {
		return nil, err
	}

	return n, nil
}

// ensureTx returns the caller's transaction, or opens one when the caller
// passed nil.
//
// commit and release are both no-ops for a borrowed transaction -- the caller
// still owns its lifetime, and rolling it back here would abort work this store
// knows nothing about.
func (s *Store) ensureTx(ctx context.Context, tx *sql.Tx) (_ *sql.Tx, commit func() error, release func(), _ error) {
	if tx != nil {
		return tx, func() error { return nil }, func() {}, nil
	}

	own, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, nil, err
	}

	return own, own.Commit, func() { sqlutil.Rollback(ctx, "service: write", own) }, nil
}

func (s *Store) DeleteManyTx(ctx context.Context, tx *sql.Tx, ids []string) error {
	err := permission.LimitCheckAny(ctx, permission.Admin, permission.User)
	if err != nil {
		return err
	}
	err = validate.ManyUUID("ServiceID", ids, 50)
	if err != nil {
		return err
	}
	stmt := s.delete
	if tx != nil {
		stmt = tx.StmtContext(ctx, stmt)
	}
	_, err = stmt.ExecContext(ctx, sqlutil.UUIDArray(ids))
	return err
}

func wrap(tx *sql.Tx, s *sql.Stmt) *sql.Stmt {
	if tx == nil {
		return s
	}
	return tx.Stmt(s)
}

func (s *Store) UpdateTx(ctx context.Context, tx *sql.Tx, svc *Service) error {
	err := permission.LimitCheckAny(ctx, permission.Admin, permission.User)
	if err != nil {
		return err
	}

	tx, commit, release, err := s.ensureTx(ctx, tx)
	if err != nil {
		return err
	}
	defer release()

	n, err := svc.Normalize()
	if err != nil {
		return err
	}

	err = validate.UUID("ServiceID", n.ID)
	if err != nil {
		return err
	}

	mExp := sql.NullTime{
		Time:  n.MaintenanceExpiresAt,
		Valid: !n.MaintenanceExpiresAt.IsZero(),
	}

	_, err = tx.StmtContext(ctx, s.update).ExecContext(ctx, n.ID, n.Name, n.Description, n.EscalationPolicyID, mExp, n.AlertScheduleEnabled, newNullString(n.NotificationTimeZone))
	if err != nil {
		return err
	}

	// Rules are replaced wholesale so the switch, timezone and windows always
	// change together -- a service can never be briefly enabled with no windows.
	err = replaceNotificationRules(ctx, gadb.New(tx), n)
	if err != nil {
		return err
	}

	return commit()
}

func (s *Store) FindOneForUser(ctx context.Context, userID, serviceID string) (*Service, error) {
	err := validate.UUID("ServiceID", serviceID)
	if err != nil {
		return nil, err
	}

	var uid sql.NullString
	userCheck := permission.User

	if userID != "" {
		err := validate.UUID("UserID", userID)
		if err != nil {
			return nil, err
		}
		userCheck = permission.MatchUser(userID)
		uid.Valid = true
		uid.String = userID
	}

	err = permission.LimitCheckAny(ctx, userCheck, permission.System)
	if err != nil {
		return nil, err
	}

	row := s.findOne.QueryRowContext(ctx, serviceID, uid)
	var svc Service
	err = scanFrom(&svc, row.Scan)
	if err != nil {
		return nil, err
	}

	one := []Service{svc}
	err = loadNotificationRules(ctx, gadb.New(s.db), one)
	if err != nil {
		return nil, err
	}

	return &one[0], nil
}

func (s *Store) FindOne(ctx context.Context, id string) (*Service, error) {
	// old method just calls new method
	return s.FindOneForUser(ctx, "", id)
}

func scanFrom(s *Service, f func(args ...interface{}) error) error {
	var maintExpiresAt sql.NullTime
	var tz sql.NullString
	err := f(&s.ID, &s.Name, &s.Description, &s.EscalationPolicyID, &s.epName, &s.isUserFavorite, &maintExpiresAt, &s.AlertScheduleEnabled, &tz, &s.NotificationSuppressed)
	if err != nil {
		return err
	}
	s.MaintenanceExpiresAt = maintExpiresAt.Time
	s.NotificationTimeZone = tz.String
	return nil
}

func newNullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

func scanAllFrom(rows *sql.Rows) (services []Service, err error) {
	var s Service
	for rows.Next() {
		err = scanFrom(&s, rows.Scan)
		if err != nil {
			return nil, err
		}
		services = append(services, s)
	}
	return services, nil
}

func (s *Store) FindAllByEP(ctx context.Context, epID string) ([]Service, error) {
	err := permission.LimitCheckAny(ctx, permission.Admin, permission.User)
	if err != nil {
		return nil, err
	}

	rows, err := s.findAllByEP.QueryContext(ctx, epID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return s.scanAllWithRules(ctx, rows)
}

func (s *Store) scanAllWithRules(ctx context.Context, rows *sql.Rows) ([]Service, error) {
	services, err := scanAllFrom(rows)
	if err != nil {
		return nil, err
	}

	err = loadNotificationRules(ctx, gadb.New(s.db), services)
	if err != nil {
		return nil, err
	}

	return services, nil
}
