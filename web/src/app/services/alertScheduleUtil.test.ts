import { gqlClockTimeToISO, isoToGQLClockTime } from '../schedules/util'
import {
  alertScheduleInput,
  newAlertScheduleRule,
  remapRulesToZone,
  rulesToGQL,
} from './alertScheduleUtil'

const weekdays = [false, true, true, true, true, true, false]

function ruleAt(
  clock: string,
  zone: string,
): {
  start: string
  end: string
  weekdayFilter: boolean[]
} {
  return {
    start: gqlClockTimeToISO(clock, zone),
    end: gqlClockTimeToISO(clock, zone),
    weekdayFilter: weekdays,
  }
}

describe('remapRulesToZone', () => {
  it('keeps the wall-clock time the user is looking at', () => {
    const rule = ruleAt('09:00', 'America/Chicago')

    const [moved] = remapRulesToZone(
      [rule],
      'America/Chicago',
      'America/New_York',
    )

    expect(isoToGQLClockTime(moved.start, 'America/New_York')).toBe('09:00')
    expect(rulesToGQL([moved], 'America/New_York')[0].start).toBe('09:00')
  })

  it('is what stops a zone change from shifting the window', () => {
    const rule = ruleAt('09:00', 'America/Chicago')

    // Without remapping, the same instant read in a different zone is a
    // different wall-clock time -- this is the shift being guarded against.
    expect(isoToGQLClockTime(rule.start, 'America/New_York')).toBe('10:00')
  })

  it('anchors to browser-local time when no zone was chosen yet', () => {
    const seeded = newAlertScheduleRule()

    const [moved] = remapRulesToZone([seeded], undefined, 'UTC')

    // The seeded default is local midnight, and it should still read as
    // midnight after the first zone is picked.
    expect(isoToGQLClockTime(moved.start, 'UTC')).toBe('00:00')
  })

  it('preserves the weekday filter', () => {
    const [moved] = remapRulesToZone([ruleAt('09:00', 'UTC')], 'UTC', 'UTC')
    expect(moved.weekdayFilter).toEqual(weekdays)
  })
})

describe('newAlertScheduleRule', () => {
  it('defaults to a full day in the alerting zone', () => {
    const rule = newAlertScheduleRule('Asia/Kolkata')

    expect(isoToGQLClockTime(rule.start, 'Asia/Kolkata')).toBe('00:00')
    // start === end is how the backend spells "24 hours".
    expect(rule.start).toBe(rule.end)
  })
})

describe('alertScheduleInput', () => {
  it('sends no windows when alerting windows are off', () => {
    expect(alertScheduleInput(false, 'UTC', [ruleAt('09:00', 'UTC')])).toEqual({
      alertScheduleEnabled: false,
    })
    expect(
      alertScheduleInput(undefined, 'UTC', [ruleAt('09:00', 'UTC')]),
    ).toEqual({ alertScheduleEnabled: false })
  })

  it('sends the zone and windows when they are on', () => {
    const input = alertScheduleInput(true, 'UTC', [ruleAt('09:00', 'UTC')])

    expect(input).toEqual({
      alertScheduleEnabled: true,
      notificationTimeZone: 'UTC',
      notificationRules: [
        { start: '09:00', end: '09:00', weekdayFilter: weekdays },
      ],
    })
  })
})
