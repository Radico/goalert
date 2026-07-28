import React, { useState, useContext } from 'react'
import { useMutation, gql } from 'urql'
import Typography from '@mui/material/Typography'
import FormDialog from '../../dialogs/FormDialog'
import { FormContainer, FormField } from '../../forms'
import { UserSelect } from '../../selection'
import { NotificationContext } from '../../main/SnackbarNotification'

const updateMutation = gql`
  mutation UpdateAlertsMutation($input: UpdateAlertsInput!) {
    updateAlerts(input: $input) {
      id
    }
  }
`

interface AlertReassignDialogProps {
  open: boolean
  onClose: () => void
  alertIDs: Array<string>
}

export default function AlertReassignDialog(
  props: AlertReassignDialogProps,
): React.JSX.Element | null {
  const { alertIDs, open, onClose } = props

  const [value, setValue] = useState<{ assignedUserID: string }>({
    assignedUserID: '',
  })
  const [mutationStatus, commit] = useMutation(updateMutation)
  const { error, fetching } = mutationStatus

  const { setNotification } = useContext(NotificationContext)

  if (!open) return null

  function submit(input: Record<string, unknown>): void {
    commit({ input: { alertIDs, ...input } }).then((result) => {
      if (result.error) return

      const count = alertIDs.length
      const numUpdated = result.data?.updateAlerts?.length ?? 0

      setNotification({
        message: `${numUpdated} of ${count} alert${
          count === 1 ? '' : 's'
        } updated`,
        severity: 'info',
      })

      setValue({ assignedUserID: '' })
      onClose()
    })
  }

  return (
    <FormDialog
      title='Assign Alerts'
      subTitle={
        'Assigning records who owns these alerts. It does not change who is ' +
        'notified — that is always determined by the escalation policy.'
      }
      loading={fetching}
      errors={error ? [{ message: error.message }] : []}
      onClose={onClose}
      caption='Leave blank to clear the assignment and return these alerts to tracking the on-call user.'
      primaryActionLabel={value.assignedUserID ? 'Assign' : 'Clear Assignment'}
      onSubmit={() =>
        submit(
          value.assignedUserID
            ? { assignedUserID: value.assignedUserID }
            : { clearAssignment: true },
        )
      }
      form={
        <FormContainer value={value} onChange={setValue}>
          <Typography sx={{ pb: 2 }} variant='body2'>
            Alerts: {alertIDs.join(', ')}
          </Typography>
          <FormField
            component={UserSelect}
            fieldName='assignedUserID'
            name='assignedUserID'
            label='Assign to'
            fullWidth
          />
        </FormContainer>
      }
    />
  )
}
