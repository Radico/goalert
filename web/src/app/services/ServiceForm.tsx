import React from 'react'
import Grid from '@mui/material/Grid'
import TextField from '@mui/material/TextField'
import { EscalationPolicySelect } from '../selection/EscalationPolicySelect'
import { FormContainer, FormField } from '../forms'
import { useConfigValue } from '../util/RequireConfig'
import { Label, ServiceUrgency } from '../../schema'
import { InputAdornment, MenuItem } from '@mui/material'
import { ExpFlag, useExpFlag } from '../util/useExpFlag'
import ServiceAlertScheduleForm from './ServiceAlertScheduleForm'
import {
  AlertScheduleRule,
  newAlertScheduleRule,
  remapRulesToZone,
} from './alertScheduleUtil'

const MaxDetailsLength = 6 * 1024 // 6KiB

const urgencyOptions: { value: ServiceUrgency; label: string; hint: string }[] =
  [
    {
      value: 'high',
      label: 'High',
      hint: 'Alerts run the escalation policy and notify on-call users.',
    },
    {
      value: 'low',
      label: 'Low',
      hint: 'Alerts are recorded for tracking only; nobody is notified.',
    },
    {
      value: 'scheduled',
      label: 'Scheduled',
      hint: 'Alerts notify only during the windows below. Outside them they are recorded only, and notify when the next window opens.',
    },
  ]

export interface Value {
  name: string
  description: string
  escalationPolicyID?: string
  notificationUrgency?: ServiceUrgency
  notificationTimeZone?: string
  notificationRules?: AlertScheduleRule[]
  labels: Label[]
}

interface ServiceFormProps {
  value: Value

  nameError?: string
  descError?: string
  epError?: string

  labelErrorKey?: string
  labelErrorMsg?: string

  onChange: (val: Value) => void

  disabled?: boolean

  epRequired?: boolean
}

export default function ServiceForm(props: ServiceFormProps): JSX.Element {
  const {
    epRequired,
    nameError,
    descError,
    epError,
    labelErrorKey,
    labelErrorMsg,
    ...containerProps
  } = props

  const formErrs = [
    { field: 'name', message: nameError },
    { field: 'description', message: descError },
    {
      field: 'escalationPolicyID',
      message: epError,
    },
    {
      field: 'label_' + labelErrorKey,
      message: labelErrorMsg,
    },
  ].filter((e) => e.message)

  const [reqLabels] = useConfigValue('Services.RequiredLabels') as [string[]]
  const alertScheduleEnabled = useExpFlag('svc-alert-schedule')

  // Hide the Scheduled option unless the flag is on -- but keep it visible for
  // a service already using it, so its urgency is never silently misreported.
  const options = urgencyOptions.filter(
    (o) =>
      o.value !== 'scheduled' ||
      alertScheduleEnabled ||
      props.value.notificationUrgency === 'scheduled',
  )

  const handleChange = (val: Value): void => {
    const prevZone = props.value.notificationTimeZone
    const nextZone = val.notificationTimeZone

    // Window times are wall-clock in the alerting zone. Changing the zone must
    // keep the times the user is looking at, not silently reinterpret them.
    if (nextZone && nextZone !== prevZone && val.notificationRules?.length) {
      val = {
        ...val,
        notificationRules: remapRulesToZone(
          val.notificationRules,
          prevZone,
          nextZone,
        ),
      }
    }

    // Selecting Scheduled with no windows would leave the table empty and the
    // service unable to save; seed one as soon as the mode is picked.
    if (
      val.notificationUrgency === 'scheduled' &&
      !val.notificationRules?.length
    ) {
      val = {
        ...val,
        notificationRules: [newAlertScheduleRule(nextZone)],
      }
    }

    props.onChange(val)
  }

  return (
    <FormContainer
      {...containerProps}
      onChange={handleChange}
      errors={formErrs}
      optionalLabels={epRequired}
    >
      <Grid container spacing={2}>
        <Grid item xs={12}>
          <FormField
            fullWidth
            label='Name'
            name='name'
            required
            component={TextField}
          />
        </Grid>
        <Grid item xs={12}>
          <FormField
            fullWidth
            label='Description'
            name='description'
            multiline
            rows={4}
            component={TextField}
            charCount={MaxDetailsLength}
            hint='Markdown Supported'
          />
        </Grid>
        <Grid item xs={12}>
          <FormField
            fullWidth
            label='Escalation Policy'
            name='escalation-policy'
            fieldName='escalationPolicyID'
            required={epRequired}
            component={EscalationPolicySelect}
          />
        </Grid>
        <Grid item xs={12}>
          <FormField
            fullWidth
            component={TextField}
            select
            label='Urgency'
            name='notificationUrgency'
            hint={
              options.find(
                (o) => o.value === (props.value.notificationUrgency || 'high'),
              )?.hint
            }
          >
            {options.map((o) => (
              <MenuItem value={o.value} key={o.value}>
                {o.label}
              </MenuItem>
            ))}
          </FormField>
        </Grid>
        {props.value.notificationUrgency === 'scheduled' && (
          <ExpFlag flag='svc-alert-schedule'>
            <ServiceAlertScheduleForm
              disabled={props.disabled}
              timeZone={props.value.notificationTimeZone}
              rules={props.value.notificationRules ?? []}
              onChangeRules={(notificationRules) =>
                props.onChange({ ...props.value, notificationRules })
              }
            />
          </ExpFlag>
        )}
        {reqLabels &&
          reqLabels.map((labelName: string, idx: number) => (
            <Grid item xs={12} key={labelName}>
              <FormField
                fullWidth
                name={labelName}
                required={!epRequired} // optional when editing
                component={TextField}
                fieldName='labels'
                errorName={'label_' + labelName}
                label={
                  reqLabels.length === 1
                    ? 'Service Label'
                    : reqLabels.length > 1 && idx === 0
                      ? 'Service Labels'
                      : ''
                }
                InputProps={{
                  startAdornment: (
                    <InputAdornment position='start'>
                      {labelName}:
                    </InputAdornment>
                  ),
                }}
                mapOnChangeValue={(newVal: string, value: Value) => {
                  return [
                    ...value.labels.filter((l) => l.key !== labelName),
                    {
                      key: labelName,
                      value: newVal,
                    },
                  ]
                }}
                mapValue={(labels: Label[]) =>
                  labels.find((l) => l.key === labelName)?.value || ''
                }
              />
            </Grid>
          ))}
      </Grid>
    </FormContainer>
  )
}
