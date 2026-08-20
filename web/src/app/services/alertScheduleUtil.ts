import { DateTime } from 'luxon'
import { ServiceNotificationRule } from '../../schema'
import { gqlClockTimeToISO, isoToGQLClockTime } from '../schedules/util'

// A weekly alerting window as the form holds it: ISO instants, because that is
// what ISOTimePicker edits. They only mean anything relative to the service's
// alerting zone -- see remapRulesToZone.
export type AlertScheduleRule = {
  start: string
  end: string
  weekdayFilter: boolean[]
}

// A rule spanning midnight-to-midnight is a full 24 hours, which is what a
// newly added rule should default to -- the same convention the backend uses
// for start == end.
//
// Midnight is resolved in the alerting zone, not the browser's, so the saved
// wall-clock time is the one the user sees.
export function newAlertScheduleRule(zone?: string): AlertScheduleRule {
  const midnight = (zone ? DateTime.local().setZone(zone) : DateTime.local())
    .startOf('day')
    .toUTC()
    .toISO()

  return {
    start: midnight,
    end: midnight,
    weekdayFilter: [false, true, true, true, true, true, false],
  }
}

// The API speaks ClockTime ("HH:mm" in the service's zone) while ISOTimePicker
// works in ISO timestamps, so the two are converted at the dialog boundary --
// the same split ScheduleRuleForm uses.

export function rulesFromGQL(
  rules: ServiceNotificationRule[] | undefined,
  zone: string | undefined,
): AlertScheduleRule[] {
  if (!rules?.length || !zone) return []

  return rules.map((r) => ({
    start: gqlClockTimeToISO(r.start, zone),
    end: gqlClockTimeToISO(r.end, zone),
    weekdayFilter: [...r.weekdayFilter],
  }))
}

export function rulesToGQL(
  rules: AlertScheduleRule[] | undefined,
  zone: string | undefined,
): ServiceNotificationRule[] {
  if (!rules?.length || !zone) return []

  return rules.map((r) => ({
    start: isoToGQLClockTime(r.start, zone),
    end: isoToGQLClockTime(r.end, zone),
    weekdayFilter: r.weekdayFilter,
  })) as ServiceNotificationRule[]
}

// remapRulesToZone re-anchors rules so the wall-clock times the user is looking
// at survive a change of alerting time zone.
//
// The form holds ISO instants (that is what ISOTimePicker edits) but the API
// stores wall-clock times, so an instant only means something relative to a
// zone. ISOTimePicker re-derives its display from the zone it is given, but it
// does not push the re-derived value back up -- so without this the stored
// window silently shifts by the offset between the browser and the alerting
// zone. It reproduces intermittently, which is what makes it worth pinning.
export function remapRulesToZone(
  rules: AlertScheduleRule[],
  fromZone: string | undefined,
  toZone: string,
): AlertScheduleRule[] {
  // Before a zone is chosen the pickers are showing browser-local time, so that
  // is what the displayed times are anchored to.
  const from = fromZone || DateTime.local().zoneName

  return rules.map((r) => ({
    ...r,
    start: gqlClockTimeToISO(isoToGQLClockTime(r.start, from), toZone),
    end: gqlClockTimeToISO(isoToGQLClockTime(r.end, from), toZone),
  }))
}

// alertScheduleInput returns the schedule-related fields of a service mutation.
// The zone and windows are only sent when alerting windows are on, since the
// backend clears them otherwise.
export function alertScheduleInput(
  enabled: boolean | undefined,
  zone: string | undefined,
  rules: AlertScheduleRule[] | undefined,
): Record<string, unknown> {
  if (!enabled) return { alertScheduleEnabled: false }

  return {
    alertScheduleEnabled: true,
    notificationTimeZone: zone || '',
    notificationRules: rulesToGQL(rules, zone),
  }
}
