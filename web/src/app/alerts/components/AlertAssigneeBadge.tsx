import React from 'react'
import Chip from '@mui/material/Chip'
import PersonIcon from '@mui/icons-material/Person'

export interface AlertAssignee {
  assignedUser?: { id: string; name: string } | null
  assignmentSource?: string | null
}

interface AlertAssigneeBadgeProps {
  alert: AlertAssignee
  /** Whether the assignment feature is enabled; when false nothing renders. */
  enabled: boolean
}

/*
 * Shows who owns an alert.
 *
 * Only *explicit* assignments get a badge. Unclaimed alerts always resolve to
 * whoever is on-call, so badging those too would put a chip on nearly every row
 * and drown out the signal this is meant to carry: that a person has actually
 * picked this alert up.
 */
export default function AlertAssigneeBadge(
  props: AlertAssigneeBadgeProps,
): React.JSX.Element | null {
  const { alert, enabled } = props

  if (!enabled) return null
  if (alert.assignmentSource !== 'explicit') return null
  if (!alert.assignedUser) return null

  return (
    <Chip
      size='small'
      variant='outlined'
      color='primary'
      icon={<PersonIcon />}
      label={alert.assignedUser.name}
      data-cy='alert-assignee-badge'
      aria-label={`Assigned to ${alert.assignedUser.name}`}
      sx={{ ml: 1, verticalAlign: 'middle' }}
    />
  )
}
