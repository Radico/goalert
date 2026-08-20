import React from 'react'
import Grid from '@mui/material/Grid'
import TextField from '@mui/material/TextField'
import { EscalationPolicySelect } from '../selection/EscalationPolicySelect'
import { FormContainer, FormField } from '../forms'
import { useConfigValue } from '../util/RequireConfig'
import { Label } from '../../schema'
import {
  Checkbox,
  FormControlLabel,
  FormHelperText,
  InputAdornment,
} from '@mui/material'
import { ExpFlag, useExpFlag } from '../util/useExpFlag'
import ServiceAlertScheduleForm from './ServiceAlertScheduleForm'
import {
  AlertScheduleRule,
  newAlertScheduleRule,
  remapRulesToZone,
} from './alertScheduleUtil'

const MaxDetailsLength = 6 * 1024 // 6KiB

export interface Value {
  name: string
  description: string
  escalationPolicyID?: string
  alertScheduleEnabled?: boolean
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

    // Turning it on with no windows would leave the table empty and the service
    // unable to save; seed one as soon as the box is ticked.
    if (val.alertScheduleEnabled && !val.notificationRules?.length) {
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
        {(alertScheduleEnabled || props.value.alertScheduleEnabled) && (
          <ExpFlag flag='svc-alert-schedule'>
            <Grid item xs={12}>
              <FormControlLabel
                label='Only alert during scheduled windows'
                control={
                  <FormField
                    component={Checkbox}
                    checkbox
                    noError
                    name='alertScheduleEnabled'
                  />
                }
              />
              <FormHelperText>
                Outside these windows alerts are recorded but nobody is
                notified. Anything captured then notifies when the next window
                opens.
              </FormHelperText>
            </Grid>
            {props.value.alertScheduleEnabled && (
              <ServiceAlertScheduleForm
                disabled={props.disabled}
                timeZone={props.value.notificationTimeZone}
                rules={props.value.notificationRules ?? []}
                onChangeRules={(notificationRules) =>
                  handleChange({ ...props.value, notificationRules })
                }
              />
            )}
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
