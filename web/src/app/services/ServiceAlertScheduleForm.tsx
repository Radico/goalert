import React from 'react'
import {
  Checkbox,
  Grid,
  Hidden,
  IconButton,
  MenuItem,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableRow,
  TextField,
  Typography,
} from '@mui/material'
import { DateTime } from 'luxon'
import { FormField } from '../forms'
import { TimeZoneSelect } from '../selection'
import { Add, Trash } from '../icons'
import { ISOTimePicker } from '../util/ISOPickers'
import { fmtLocal } from '../util/timeFormat'
import { days, weekdaySummary } from '../schedules/util'
import { AlertScheduleRule, newAlertScheduleRule } from './alertScheduleUtil'

// Reuse the schedules summary so the collapsed day list reads the same here as
// it does everywhere else ("M-F" rather than "Mon, Tue, Wed, Thu, Fri").
const renderDaysValue = (value: string[]): string =>
  weekdaySummary(days.map((d: string) => value.includes(d)))

interface ServiceAlertScheduleFormProps {
  // rules is read directly (rather than only through FormField) so the table
  // knows how many rows to render and whether deleting is allowed.
  rules: AlertScheduleRule[]
  timeZone?: string
  onChangeRules: (rules: AlertScheduleRule[]) => void
  disabled?: boolean
}

// ServiceAlertScheduleForm renders the weekly alerting windows for a service.
// It is rendered inside the parent ServiceForm's FormContainer, so field names
// are paths into the service value rather than a nested form.
export default function ServiceAlertScheduleForm(
  props: ServiceAlertScheduleFormProps,
): JSX.Element {
  const { rules, timeZone, onChangeRules, disabled } = props
  const isLocalZone = !timeZone || timeZone === DateTime.local().zoneName

  return (
    <React.Fragment>
      <Grid item xs={12}>
        <FormField
          fullWidth
          component={TimeZoneSelect}
          name='notificationTimeZone'
          fieldName='notificationTimeZone'
          label='Alerting Time Zone'
          required
          disabled={disabled}
        />
      </Grid>
      <Grid item xs={12}>
        <Typography color='textSecondary' sx={{ fontStyle: 'italic' }}>
          Alerts outside these windows are recorded but not sent. They notify
          when the next window opens.
        </Typography>
      </Grid>
      <Grid item xs={12} style={{ paddingTop: 0 }}>
        <Table data-cy='alert-schedule-rules' size='small'>
          <TableHead>
            <TableRow>
              <TableCell sx={{ minWidth: '6em' }}>Start</TableCell>
              <TableCell sx={{ minWidth: '6em' }}>End</TableCell>
              <Hidden mdDown>
                {days.map((d) => (
                  <TableCell key={d} padding='checkbox'>
                    {d.slice(0, 3)}
                  </TableCell>
                ))}
              </Hidden>
              <Hidden mdUp>
                <TableCell>Days</TableCell>
              </Hidden>
              <TableCell padding='none'>
                <IconButton
                  aria-label='Add alerting window'
                  disabled={disabled}
                  onClick={() =>
                    onChangeRules(rules.concat(newAlertScheduleRule(timeZone)))
                  }
                  size='large'
                >
                  <Add />
                </IconButton>
              </TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {rules.map((rule, idx) => (
              <TableRow key={idx}>
                <TableCell sx={{ minWidth: '6em' }}>
                  <FormField
                    fullWidth
                    noError
                    component={ISOTimePicker}
                    required
                    label=''
                    name={`notificationRules[${idx}].start`}
                    disabled={!timeZone || disabled}
                    timeZone={timeZone}
                    hint={isLocalZone ? '' : fmtLocal(rule.start)}
                  />
                </TableCell>
                <TableCell sx={{ minWidth: '6em' }}>
                  <FormField
                    fullWidth
                    noError
                    component={ISOTimePicker}
                    required
                    label=''
                    name={`notificationRules[${idx}].end`}
                    disabled={!timeZone || disabled}
                    timeZone={timeZone}
                    hint={isLocalZone ? '' : fmtLocal(rule.end)}
                  />
                </TableCell>
                <Hidden mdDown>
                  {days.map((day, dayIdx) => (
                    <TableCell key={dayIdx} padding='checkbox'>
                      <FormField
                        noError
                        component={Checkbox}
                        checkbox
                        disabled={disabled}
                        fieldName={`notificationRules[${idx}].weekdayFilter[${dayIdx}]`}
                        name={day}
                      />
                    </TableCell>
                  ))}
                </Hidden>
                <Hidden mdUp>
                  <TableCell>
                    <FormField
                      fullWidth
                      component={TextField}
                      select
                      noError
                      required
                      disabled={disabled}
                      SelectProps={{
                        renderValue: renderDaysValue,
                        multiple: true,
                      }}
                      label=''
                      name={`notificationRules[${idx}].weekdayFilter`}
                      aria-label='Weekday Filter'
                      multiple
                      mapValue={(value: boolean[]) =>
                        days.filter((d, i) => value[i])
                      }
                      mapOnChangeValue={(value: string[]) =>
                        days.map((day) => value.includes(day))
                      }
                    >
                      {days.map((day) => (
                        <MenuItem value={day} key={day}>
                          {day}
                        </MenuItem>
                      ))}
                    </FormField>
                  </TableCell>
                </Hidden>
                <TableCell padding='none'>
                  {rules.length > 1 && (
                    <IconButton
                      aria-label='Delete alerting window'
                      disabled={disabled}
                      onClick={() =>
                        onChangeRules(rules.filter((r, i) => i !== idx))
                      }
                      size='large'
                    >
                      <Trash />
                    </IconButton>
                  )}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </Grid>
    </React.Fragment>
  )
}
