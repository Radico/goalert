package alert

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"text/template"
	"time"

	"github.com/target/goalert/permission"
	"github.com/target/goalert/search"
	"github.com/target/goalert/util/sqlutil"
	"github.com/target/goalert/validation/validate"

	"github.com/pkg/errors"
)

// SortMode indicates the mode of sorting for alerts.
type SortMode int

const (
	// SortModeStatusID will sort by status priority (unacked, then acked, then closed) followed by ID (newest/highest first)
	SortModeStatusID SortMode = iota

	// SortModeDateID will sort alerts by date newest first, falling back to ID (newest/highest first)
	SortModeDateID

	// SortModeDateIDReverse will sort alerts by date oldest first, falling back to ID (oldest/lowest first)
	SortModeDateIDReverse
)

// SearchOptions contains criteria for filtering and sorting alerts.
type SearchOptions struct {
	// Search is matched case-insensitive against the alert summary, id and service name.
	Search string `json:"s,omitempty"`

	// Status, if specified, will restrict alerts to those with a matching status.
	Status []Status `json:"t,omitempty"`

	// ServiceFilter, if specified, will restrict alerts to those with a matching ServiceID on IDs, if valid.
	ServiceFilter IDFilter `json:"v,omitempty"`

	After SearchCursor `json:"a,omitempty"`

	// Omit specifies a list of alert IDs to exclude from the results.
	Omit []int `json:"o,omitempty"`

	// NotifiedUserID will include all alerts the specified user has been
	// notified for to the results.
	NotifiedUserID string `json:"e,omitempty"`

	// Limit restricts the maximum number of rows returned. Default is 50.
	// Note: Limit is applied AFTER AfterID is taken into account.
	Limit int `json:"-"`

	// Sort allows customizing the sort method.
	Sort SortMode `json:"z,omitempty"`

	// NotBefore will omit any alerts created any time before the provided time.
	NotBefore time.Time `json:"n,omitempty"`

	// Before will only include alerts that were created before the provided time.
	Before time.Time `json:"b,omitempty"`

	// serviceNameIDs is used internally to store IDs for services matching the query name.
	serviceNameIDs []string

	// ClosedBefore will only include alerts that were closed before the provided time.
	ClosedBefore time.Time `json:"c,omitempty"`

	// NotClosedBefore will omit any alerts closed any time before the provided time.
	NotClosedBefore time.Time `json:"nc,omitempty"`

	// AssignedUserID, if specified, will restrict alerts to those assigned to
	// the given user -- either explicitly (claimed) or by virtue of them being
	// on-call for the alert.
	//
	// Unlike NotifiedUserID this is restrictive rather than additive.
	AssignedUserID string `json:"au,omitempty"`

	// IncludeAssignedUserID will add all alerts assigned to the specified user
	// to the results, the same way NotifiedUserID does.
	//
	// This is the additive counterpart to AssignedUserID; the two are mutually
	// exclusive and Normalize clears this one when both are set.
	IncludeAssignedUserID string `json:"ia,omitempty"`

	// OnCallUserID will add every alert belonging to a service the given user is
	// the primary on-call for -- the earliest step of the service's escalation
	// policy that has anybody on it -- the same way NotifiedUserID does. Like NotifiedUserID it widens
	// a service-scoped search and has no effect on one that is not scoped to
	// services at all.
	//
	// NotifiedUserID alone covers only alerts that already paged the user,
	// which is a record of what happened rather than of what they are
	// responsible for now. It misses an open alert on their service that was
	// claimed by the previous rotation, or that arrived while the service was
	// not notifying.
	OnCallUserID string `json:"oc,omitempty"`

	// AssignmentSources, if specified, restricts results to alerts whose
	// ownership came from one of the given sources -- claimed explicitly,
	// derived from being on-call, or owned by nobody.
	//
	// It narrows AssignedUserID when both are given, and stands on its own
	// otherwise: sources alone answer questions like "which alerts belong to
	// nobody at all".
	AssignmentSources []AssignmentSource `json:"as,omitempty"`

	// OnCallAnyStep widens OnCallUserID from the first escalation step to every
	// step, covering services that would only reach the user if an alert
	// escalated far enough.
	//
	// Being further down a policy is not the same as being on-call for the
	// service, so this is opt-in: it answers "what could reach me tonight"
	// rather than "what am I responsible for now".
	OnCallAnyStep bool `json:"oca,omitempty"`
}

type IDFilter struct {
	Valid bool     `json:"v,omitempty"`
	IDs   []string `json:"i,omitempty"`
}

type SearchCursor struct {
	ID      int       `json:"i,omitempty"`
	Status  Status    `json:"s,omitempty"`
	Created time.Time `json:"c,omitempty"`
}

var serviceSearchTemplate = template.Must(template.New("alert-search-services").Funcs(search.Helpers()).Parse(`
	SELECT id
	FROM services
	WHERE {{textSearch "search" "name"}}
`))

var searchTemplate = template.Must(template.New("alert-search").Funcs(search.Helpers()).Parse(`
	{{/*
		Matches alerts owned by :assignedUserID -- either claimed by them, or
		unclaimed and derived from them being on-call for the earliest step the
		alert has not already escalated past.

		Defined once because it is used in both the restrictive and the additive
		mode, and because it must use the same selection rule as
		Alert_GetManyOnCallAssignees so that the alerts listed are exactly the
		ones that display as assigned to that user.
	*/}}
	{{/* The user an unclaimed alert resolves to, or null if it resolves to nobody. */}}
	{{ define "onCallOwner" }}
		(
			select ocu.user_id
			from escalation_policy_state st
			join escalation_policy_steps step on
				step.escalation_policy_id = st.escalation_policy_id and
				step.step_number >= st.escalation_policy_step_number
			join ep_step_on_call_users ocu on
				ocu.ep_step_id = step.id and
				ocu.end_time isnull
			where st.alert_id = a.id
			order by step.step_number, ocu.start_time, ocu.id
			limit 1
		)
	{{ end }}
	{{ define "assignedToUser" }}
		(
			a.assigned_user_id = :assignedUserID::uuid
			OR (
				a.assigned_user_id isnull
				AND :assignedUserID::uuid = {{ template "onCallOwner" . }}
			)
		)
	{{ end }}
	{{/*
		Ownership restricted by where it came from, mirroring the
		AlertAssignmentSource an alert reports.

		Sources are evaluated as a union, so an empty filter is every source and
		behaves exactly like assignedToUser. 'unassigned' describes an alert
		belonging to nobody, so it cannot also belong to a named user --
		SrcUnassigned reports false whenever one is given, leaving a filter for
		just that combination matching nothing rather than quietly widening.
	*/}}
	{{ define "ownershipMatch" }}
		(
			false
			{{ if .SrcExplicit }}
				OR a.assigned_user_id {{ if .AssignedUserID }}= :assignedUserID::uuid{{ else }}notnull{{ end }}
			{{ end }}
			{{ if .SrcOnCall }}
				OR (
					a.assigned_user_id isnull
					AND {{ template "onCallOwner" . }} {{ if .AssignedUserID }}= :assignedUserID::uuid{{ else }}notnull{{ end }}
				)
			{{ end }}
			{{ if .SrcUnassigned }}
				OR (
					a.assigned_user_id isnull
					AND {{ template "onCallOwner" . }} isnull
				)
			{{ end }}
		)
	{{ end }}
	{{/*
		Services the user is on-call for right now.

		The primary responder is whoever is on-call for the earliest step that
		has anybody on it, not literally step 0: a first step covering an
		uncovered rotation, or targeting only a notification channel, pages
		nobody, and the responsibility belongs to the first step that does
		reach a person. That matches how Alert_GetManyOnCallAssignees resolves
		ownership, so the service a user sees and the alerts naming them as
		owner stay in agreement.

		OnCallAnyStep drops the restriction to cover every step the user sits
		on, however far down.
	*/}}
	{{ define "onCallServices" }}
		a.service_id = any(
			select svc.id
			from ep_step_on_call_users oc
			join escalation_policy_steps step on step.id = oc.ep_step_id
			join services svc on svc.escalation_policy_id = step.escalation_policy_id
			where
				oc.user_id = :onCallUserID::uuid and
				oc.end_time isnull
				{{ if not .OnCallAnyStep }}
				and step.step_number = (
					select min(staffed.step_number)
					from escalation_policy_steps staffed
					join ep_step_on_call_users soc on
						soc.ep_step_id = staffed.id and
						soc.end_time isnull
					where staffed.escalation_policy_id = step.escalation_policy_id
				)
				{{ end }}
		)
	{{ end }}
	SELECT
		a.id,
		a.summary,
		a.details,
		a.service_id,
		a.source,
		a.status,
		created_at,
		a.dedup_key
	FROM alerts a
	WHERE true
	{{ if .Omit }}
		AND not a.id = any(:omit)
	{{ end }}
	{{ if .Search }}
		AND (
			a.id = :searchID OR {{textSearch "search" "a.summary"}} OR a.service_id = any(:svcNameMatchIDs)
		)
	{{ end }}
	{{ if .Status }}
		AND a.status = any(:status::enum_alert_status[])
	{{ end }}
	{{ if .ServiceFilter.Valid }}
		AND (a.service_id = any(:services)
			{{ if .NotifiedUserID }}
				OR a.id = any(select alert_id from alert_logs where event in ('notification_sent', 'no_notification_sent') and sub_user_id = :notifiedUserID)
			{{ end }}
			{{ if .IncludeAssignedUserID }}
				OR {{ template "assignedToUser" . }}
			{{ end }}
			{{ if .OnCallUserID }}
				OR {{ template "onCallServices" . }}
			{{ end }}
		)
	{{ end }}
	{{ if not .Before.IsZero }}
		AND a.created_at < :beforeTime
	{{ end }}
	{{ if not .NotBefore.IsZero }}
		AND a.created_at >= :notBeforeTime
	{{ end }}
	{{ if .After.ID }}
		AND (
			{{ if eq .Sort 1 }}
				a.created_at < :afterCreated OR
				(a.created = :afterCreated AND a.id < :afterID)
			{{ else if eq .Sort 2}}
				a.created_at > :afterCreated OR
				(a.created_at = :afterCreated AND a.id > :afterID)
			{{ else }}
				a.status > :afterStatus::enum_alert_status OR
				(a.status = :afterStatus::enum_alert_status AND a.id < :afterID)
			{{ end }}
		)
	{{ end }}
	{{ if or .AssignedUserID .HasSourceFilter }}
		AND {{ template "ownershipMatch" . }}
	{{ end }}
	{{ if not .ClosedBefore.IsZero }}
		AND EXISTS (select 1 from alert_metrics where alert_id = a.id AND closed_at < :closedBeforeTime)
	{{ end }}
	{{ if not .NotClosedBefore.IsZero }}
		AND EXISTS (select 1 from alert_metrics where alert_id = a.id AND closed_at > :notClosedBeforeTime) 
	{{ end }}
	ORDER BY {{.SortStr}}
	LIMIT {{.Limit}}
`))

type renderData SearchOptions

func (opts renderData) SortStr() string {
	switch opts.Sort {
	case SortModeDateID:
		return "created_at DESC, id DESC"
	case SortModeDateIDReverse:
		return "created_at, id"
	}

	// SortModeStatusID
	return "status, id DESC"
}

func (opts renderData) Normalize() (*renderData, error) {
	if opts.Limit == 0 {
		opts.Limit = search.DefaultMaxResults
	}

	err := validate.Many(
		validate.Search("Search", opts.Search),
		validate.Range("Limit", opts.Limit, 0, 1001),
		validate.Range("Status", len(opts.Status), 0, 3),
		validate.ManyUUID("Services", opts.ServiceFilter.IDs, 50),
		validate.Range("Omit", len(opts.Omit), 0, 50),
		validate.OneOf("Sort", opts.Sort, SortModeStatusID, SortModeDateID, SortModeDateIDReverse),
	)
	if opts.After.Status != "" {
		err = validate.Many(err, validate.OneOf("After.Status", opts.After.Status, StatusTriggered, StatusActive, StatusClosed))
	}
	// An explicit assignee filter is restrictive, so it supersedes the additive
	// defaults rather than letting them widen it back out.
	if opts.AssignedUserID != "" {
		opts.IncludeAssignedUserID = ""
		opts.OnCallUserID = ""
		opts.OnCallAnyStep = false
	}
	if opts.AssignedUserID != "" {
		err = validate.Many(err, validate.UUID("AssignedUserID", opts.AssignedUserID))
	}
	err = validate.Many(err, validate.Range("AssignmentSources", len(opts.AssignmentSources), 0, 3))
	for i, src := range opts.AssignmentSources {
		err = validate.Many(err, validate.OneOf(fmt.Sprintf("AssignmentSources[%d]", i), src,
			AssignmentSourceUnassigned, AssignmentSourceOnCall, AssignmentSourceExplicit))
	}
	if opts.IncludeAssignedUserID != "" {
		err = validate.Many(err, validate.UUID("IncludeAssignedUserID", opts.IncludeAssignedUserID))
	}
	if opts.OnCallUserID != "" {
		err = validate.Many(err, validate.UUID("OnCallUserID", opts.OnCallUserID))
	}
	if err != nil {
		return nil, err
	}

	for i, stat := range opts.Status {
		err = validate.OneOf("Status["+strconv.Itoa(i)+"]", stat, StatusTriggered, StatusActive, StatusClosed)
		if err != nil {
			return nil, err
		}
	}

	return &opts, err
}

// assignedUser returns the user ID bound to :assignedUserID.
//
// The restrictive and additive filters share the parameter because Normalize
// makes them mutually exclusive.
func (opts renderData) assignedUser() string {
	if opts.AssignedUserID != "" {
		return opts.AssignedUserID
	}
	return opts.IncludeAssignedUserID
}

// hasSource reports whether the given source is in scope. An empty filter means
// every source, so the rendered SQL matches what assignedToUser always did.
func (opts renderData) hasSource(s AssignmentSource) bool {
	if len(opts.AssignmentSources) == 0 {
		return true
	}
	for _, v := range opts.AssignmentSources {
		if v == s {
			return true
		}
	}
	return false
}

func (opts renderData) HasSourceFilter() bool { return len(opts.AssignmentSources) > 0 }
func (opts renderData) SrcExplicit() bool     { return opts.hasSource(AssignmentSourceExplicit) }
func (opts renderData) SrcOnCall() bool       { return opts.hasSource(AssignmentSourceOnCall) }

// SrcUnassigned is never in scope alongside a user: an alert nobody owns cannot
// be owned by that user.
func (opts renderData) SrcUnassigned() bool {
	return opts.AssignedUserID == "" && opts.hasSource(AssignmentSourceUnassigned)
}

func (opts renderData) QueryArgs() []sql.NamedArg {
	var searchID sql.NullInt64
	if i, err := strconv.ParseInt(opts.Search, 10, 64); err == nil {
		searchID.Valid = true
		searchID.Int64 = i
	}

	stat := make(sqlutil.StringArray, len(opts.Status))
	for i := range opts.Status {
		stat[i] = string(opts.Status[i])
	}

	return []sql.NamedArg{
		sql.Named("search", opts.Search),
		sql.Named("searchID", searchID),
		sql.Named("status", stat),
		sql.Named("services", sqlutil.UUIDArray(opts.ServiceFilter.IDs)),
		sql.Named("svcNameMatchIDs", sqlutil.UUIDArray(opts.serviceNameIDs)),
		sql.Named("afterID", opts.After.ID),
		sql.Named("afterStatus", opts.After.Status),
		sql.Named("afterCreated", opts.After.Created),
		sql.Named("omit", sqlutil.IntArray(opts.Omit)),
		sql.Named("notifiedUserID", opts.NotifiedUserID),
		sql.Named("assignedUserID", opts.assignedUser()),
		sql.Named("onCallUserID", opts.OnCallUserID),
		sql.Named("beforeTime", opts.Before),
		sql.Named("notBeforeTime", opts.NotBefore),
		sql.Named("closedBeforeTime", opts.ClosedBefore),
		sql.Named("notClosedBeforeTime", opts.NotClosedBefore),
	}
}

func (s *Store) serviceNameSearch(ctx context.Context, data *renderData) error {
	if data.Search == "" {
		data.serviceNameIDs = nil
		return nil
	}

	query, args, err := search.RenderQuery(ctx, serviceSearchTemplate, data)
	if err != nil {
		return fmt.Errorf("render service-search query: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if errors.Is(err, sql.ErrNoRows) {
		data.serviceNameIDs = nil
		return nil
	}
	if err != nil {
		return fmt.Errorf("search for services with '%s': %w", data.Search, err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("scan service-search query: %w", err)
		}
		ids = append(ids, id)
	}

	data.serviceNameIDs = ids

	return nil
}

func (s *Store) Search(ctx context.Context, opts *SearchOptions) ([]Alert, error) {
	err := permission.LimitCheckAny(ctx, permission.System, permission.User)
	if err != nil {
		return nil, err
	}
	if opts == nil {
		opts = new(SearchOptions)
	}

	data, err := (*renderData)(opts).Normalize()
	if err != nil {
		return nil, err
	}

	err = s.serviceNameSearch(ctx, data)
	if err != nil {
		return nil, err
	}

	query, args, err := search.RenderQuery(ctx, searchTemplate, data)
	if err != nil {
		return nil, errors.Wrap(err, "render query")
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, errors.Wrap(err, "query")
	}
	defer rows.Close()

	alerts := make([]Alert, 0, opts.Limit)

	for rows.Next() {
		var a Alert
		err = errors.Wrap(a.scanFrom(rows.Scan), "scan")
		if err != nil {
			return nil, err
		}
		alerts = append(alerts, a)
	}

	return alerts, nil
}
