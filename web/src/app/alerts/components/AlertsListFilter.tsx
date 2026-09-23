import React, { useState, MouseEvent } from 'react'
import Button from '@mui/material/Button'
import IconButton from '@mui/material/IconButton'
import Popover from '@mui/material/Popover'
import FilterList from '@mui/icons-material/FilterList'
import Hidden from '@mui/material/Hidden'
import SwipeableDrawer from '@mui/material/SwipeableDrawer'
import Switch from '@mui/material/Switch'
import Grid from '@mui/material/Grid'
import makeStyles from '@mui/styles/makeStyles'
import { Theme } from '@mui/material/styles'
import { styles as globalStyles } from '../../styles/materialStyles'
import Radio from '@mui/material/Radio'
import RadioGroup from '@mui/material/RadioGroup'
import FormControlLabel from '@mui/material/FormControlLabel'
import FormControl from '@mui/material/FormControl'
import InputLabel from '@mui/material/InputLabel'
import MenuItem from '@mui/material/MenuItem'
import Select from '@mui/material/Select'
import classnames from 'classnames'
import { useURLParam, useResetURLParams } from '../../actions'
import { useIsWidthDown } from '../../util/useWidth'
import { useConfigValue, useSessionInfo } from '../../util/RequireConfig'
import { UserSelect } from '../../selection'

const useStyles = makeStyles((theme: Theme) => ({
  filterActions: globalStyles(theme).filterActions,
  drawer: {
    width: 'fit-content', // width placed on mobile drawer
  },
  grid: {
    margin: '0.5em', // margin in grid container
  },
  gridItem: {
    display: 'flex',
    alignItems: 'center', // aligns text with toggles
  },
  formControl: {
    width: '100%', // date pickers full width
  },
  popover: {
    width: '17em', // width placed on desktop popover
  },
}))

interface AlertsListFilterProps {
  allowShowAll?: boolean
}

function AlertsListFilter(props: AlertsListFilterProps): React.JSX.Element {
  const classes = useStyles()
  const [show, setShow] = useState(false)
  const [anchorEl, setAnchorEl] = useState<HTMLButtonElement | null>(null)

  const [filter, setFilter] = useURLParam<string>('filter', 'active')
  const [allServices, setAllServices] = useURLParam<boolean>(
    'allServices',
    false,
  )
  const [showAsFullTime, setShowAsFullTime] = useURLParam<boolean>(
    'fullTime',
    false,
  )
  const [assignedUserID, setAssignedUserID] = useURLParam<string>(
    'assignedUserID',
    '',
  )
  const [escalationPath, setEscalationPath] = useURLParam<boolean>(
    'escalationPath',
    false,
  )
  const [assignmentSource, setAssignmentSource] = useURLParam<string>(
    'assignmentSource',
    '',
  )
  const [assignmentEnabled, onCallServicesDisabled] = useConfigValue(
    'General.EnableAlertAssignment',
    'General.DisableOnCallServiceAlerts',
  ) as [boolean, boolean]
  const { userID: currentUserID } = useSessionInfo()
  const resetAll = useResetURLParams(
    'filter',
    'allServices',
    'fullTime',
    'assignedUserID',
    'escalationPath',
    'assignmentSource',
  ) // don't reset search param
  const isMobile = useIsWidthDown('md')
  const gridClasses = classnames(
    classes.grid,
    isMobile ? classes.drawer : classes.popover,
  )

  function handleOpenFilters(event: MouseEvent<HTMLButtonElement>): void {
    setAnchorEl(event.currentTarget)
    setShow(true)
  }

  function handleCloseFilters(): void {
    setShow(false)
  }

  function renderFilters(): React.JSX.Element {
    let favoritesFilter = null
    if (props.allowShowAll) {
      favoritesFilter = (
        <FormControlLabel
          control={
            <Switch
              aria-label='Include All Services Toggle'
              data-cy='toggle-favorites'
              checked={allServices}
              onChange={() => setAllServices(!allServices)}
            />
          }
          label='Include All Services'
        />
      )
    }

    const content = (
      <Grid container spacing={2} className={gridClasses}>
        <Grid item xs={12} className={classes.gridItem}>
          <FormControl>
            {favoritesFilter}
            <FormControlLabel
              control={
                <Switch
                  aria-label='Show full timestamps toggle'
                  name='toggle-full-time'
                  checked={showAsFullTime}
                  onChange={() => setShowAsFullTime(!showAsFullTime)}
                />
              }
              label='Show full timestamps'
            />
            {isMobile && (
              <RadioGroup
                aria-label='Alert Status Filters'
                name='status-filters'
                value={filter}
                onChange={(e) => setFilter(e.target.value)}
              >
                <FormControlLabel
                  value='active'
                  control={<Radio />}
                  label='Active'
                />
                <FormControlLabel
                  value='unacknowledged'
                  control={<Radio />}
                  label='Unacknowledged'
                />
                <FormControlLabel
                  value='acknowledged'
                  control={<Radio />}
                  label='Acknowledged'
                />
                <FormControlLabel
                  value='closed'
                  control={<Radio />}
                  label='Closed'
                />
                <FormControlLabel value='all' control={<Radio />} label='All' />
              </RadioGroup>
            )}
          </FormControl>
        </Grid>
        {/*
          The list already covers services you are the primary on-call for.
          This adds the ones that would only reach you after an escalation --
          what could land on you later, rather than what is yours now.

          Scoped to the same lists as the favorites switch: a service or policy
          alert list is already narrowed to one service or policy, so widening
          by on-call has nothing to do there.
        */}
        {props.allowShowAll && !onCallServicesDisabled && (
          <Grid item xs={12}>
            <FormControlLabel
              control={
                <Switch
                  aria-label='Include Escalation Path Services Toggle'
                  data-cy='toggle-escalation-path'
                  checked={escalationPath}
                  onChange={() => setEscalationPath(!escalationPath)}
                />
              }
              label='Include services I am on the escalation path for'
            />
          </Grid>
        )}
        {assignmentEnabled && (
          <Grid item xs={12}>
            <FormControl className={classes.formControl}>
              <UserSelect
                label='Assigned to'
                name='assignedUserID'
                data-cy='filter-assigned-user'
                value={assignedUserID}
                onChange={(id: string) => setAssignedUserID(id || '')}
              />
            </FormControl>
            {/*
              Alerts assigned to you are already included by default, so this
              narrows the list to *only* yours. Selecting yourself from the
              dropdown works too; this is just the common case.
            */}
            <Button
              size='small'
              data-cy='filter-assigned-to-me'
              disabled={!currentUserID || assignedUserID === currentUserID}
              onClick={() => setAssignedUserID(currentUserID)}
            >
              Only mine
            </Button>
            {/*
              An alert becomes yours two different ways, and they answer
              different questions: what somebody handed you, versus what is
              yours by rotation and nobody has picked up yet. Unowned belongs to
              nobody by definition, so it clears any selected user.
            */}
            <FormControl className={classes.formControl} fullWidth>
              <InputLabel id='assignment-source-label'>Ownership</InputLabel>
              <Select
                labelId='assignment-source-label'
                label='Ownership'
                data-cy='filter-assignment-source'
                value={assignmentSource}
                onChange={(e) => {
                  const next = e.target.value as string
                  if (next === 'unassigned') setAssignedUserID('')
                  setAssignmentSource(next)
                }}
              >
                <MenuItem value=''>Any</MenuItem>
                <MenuItem value='explicit'>Assigned to a person</MenuItem>
                <MenuItem value='onCall'>On-call, unclaimed</MenuItem>
                <MenuItem value='unassigned'>Owned by nobody</MenuItem>
              </Select>
            </FormControl>
          </Grid>
        )}
        <Grid item xs={12} className={classes.filterActions}>
          <Button onClick={resetAll}>Reset</Button>
          <Button onClick={handleCloseFilters}>Done</Button>
        </Grid>
      </Grid>
    )

    // renders a popover on desktop, and a swipeable drawer on mobile devices
    return (
      <React.Fragment>
        <Hidden mdDown>
          <Popover
            anchorEl={anchorEl}
            open={!!anchorEl && show}
            onClose={handleCloseFilters}
            anchorOrigin={{
              vertical: 'bottom',
              horizontal: 'right',
            }}
            transformOrigin={{
              vertical: 'top',
              horizontal: 'right',
            }}
          >
            {content}
          </Popover>
        </Hidden>
        <Hidden mdUp>
          <SwipeableDrawer
            anchor='top'
            disableDiscovery
            disableSwipeToOpen
            open={show}
            onClose={handleCloseFilters}
            onOpen={handleOpenFilters}
            SlideProps={{
              unmountOnExit: true,
            }}
          >
            {content}
          </SwipeableDrawer>
        </Hidden>
      </React.Fragment>
    )
  }

  /*
   * Finds the parent toolbar DOM node and appends the options
   * element to that node (after all the toolbar's children
   * are done being rendered)
   */

  return (
    <React.Fragment>
      <IconButton
        aria-label='Filter Alerts'
        onClick={handleOpenFilters}
        size='large'
      >
        <FilterList />
      </IconButton>
      {renderFilters()}
    </React.Fragment>
  )
}

export default AlertsListFilter
