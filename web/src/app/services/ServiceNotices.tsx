import React from 'react'
import { gql, useQuery, useMutation } from 'urql'
import { Button, Grid } from '@mui/material'
import { DateTime } from 'luxon'
import Notices, { Notice } from '../details/Notices'
import { Time } from '../util/Time'
import { ruleSummary } from '../schedules/util'

const query = gql`
  query serviceMaintenanceQuery($serviceID: ID!) {
    service(id: $serviceID) {
      maintenanceExpiresAt
      alertScheduleEnabled
      notificationTimeZone
      notificationSuppressed
      notificationRules {
        start
        end
        weekdayFilter
      }

      notices {
        type
        message
        details
      }
    }
  }
`

const mutation = gql`
  mutation updateService($input: UpdateServiceInput!) {
    updateService(input: $input)
  }
`

interface ServiceMaintenanceNoticeProps {
  serviceID: string
  extraNotices?: Notice[]
}

// assumed that this is rendered within a Grid container
export default function ServiceNotices({
  serviceID,
  extraNotices = [],
}: ServiceMaintenanceNoticeProps): JSX.Element | null {
  const [, updateService] = useMutation(mutation)
  const [{ fetching, data }] = useQuery({
    query,
    variables: { serviceID },
    pause: !serviceID,
  })

  if (!data && fetching) {
    return null
  }
  let notices = [...(data?.service?.notices ?? []), ...extraNotices]
  if (
    data?.service?.alertScheduleEnabled &&
    data?.service?.notificationSuppressed
  ) {
    notices = [
      {
        type: 'WARNING',
        message: 'Outside Alerting Window',
        details: (
          <React.Fragment>
            Alerts are being recorded, but nobody is notified. Anything captured
            now will notify when the next window opens.
            {data.service.notificationTimeZone && (
              <React.Fragment>
                <br />
                Alerting windows ({data.service.notificationTimeZone}):{' '}
                {ruleSummary(
                  data.service.notificationRules ?? [],
                  data.service.notificationTimeZone,
                  data.service.notificationTimeZone,
                )}
              </React.Fragment>
            )}
          </React.Fragment>
        ),
      },
      ...notices,
    ]
  }

  const maintMode = data?.service?.maintenanceExpiresAt
  if (maintMode) {
    notices = [
      {
        type: 'WARNING',
        message: 'In Maintenance Mode',
        details: (
          <React.Fragment>
            Ends <Time format='relative' time={maintMode} precise />
          </React.Fragment>
        ),
        action: (
          <Button
            aria-label='Cancel Maintenance Mode'
            onClick={() => {
              updateService(
                {
                  input: {
                    id: serviceID,
                    maintenanceExpiresAt: DateTime.local()
                      .minus({
                        years: 1,
                      })
                      .toISO(),
                  },
                },
                { additionalTypenames: ['Service'] },
              )
            }}
          >
            Cancel
          </Button>
        ),
      },
      ...notices,
    ]
  }

  return (
    <Grid item sx={{ width: '100%' }}>
      <Notices notices={notices} />
    </Grid>
  )
}
