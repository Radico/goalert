import { test, expect, Page } from '@playwright/test'
import { baseURLFromFlags, userSessionFile } from './lib'
import Chance from 'chance'
import { createService } from './lib/service'
const c = new Chance()

test.describe.configure({ mode: 'parallel' })
test.use({
  storageState: userSessionFile,
  baseURL: baseURLFromFlags(['svc-alert-schedule']),
})

// These select by accessible name rather than using the shared dropdownSelect
// helper, which matches on an ancestor div and is ambiguous inside the service
// form (several fields share a container).
//
// Both retry the whole open-and-pick: the Scheduled option only exists once
// useExpFlag's query resolves, so the menu can legitimately open without it.
async function selectUrgency(page: Page, option: string): Promise<void> {
  await expect(async () => {
    await page.getByRole('combobox', { name: /^Urgency/ }).click()
    await page
      .getByRole('option', { name: option, exact: true })
      .click({ timeout: 2000 })
  }).toPass({ timeout: 20000 })
}

async function selectZone(page: Page, zone: string): Promise<void> {
  await expect(async () => {
    const field = page.getByRole('combobox', { name: /Alerting Time Zone/ })
    await field.click()
    await field.fill(zone)
    await page
      .getByRole('option', { name: zone, exact: true })
      .click({ timeout: 2000 })
  }).toPass({ timeout: 20000 })
}

// Switching a service to Scheduled urgency reveals the weekly alerting window
// editor, and the configuration survives a round trip through the API.
test('configure a weekly alerting window', async ({ page, isMobile }) => {
  const name = 'pw-sched-svc ' + c.name()
  await createService(page, name, c.sentence())

  await page.getByRole('button', { name: 'Edit', exact: true }).click()
  await expect(
    page.getByRole('heading', { name: 'Edit Service' }),
  ).toBeVisible()

  await selectUrgency(page, 'Scheduled')

  // Picking Scheduled seeds one window, so the editor is immediately usable.
  const rules = page.locator('[data-cy=alert-schedule-rules]')
  await expect(rules).toBeVisible()

  // Window times are wall-clock in the alerting zone, so they are meaningless
  // until a zone is chosen -- the pickers stay disabled until then.
  const start = rules.locator('input[name="notificationRules[0].start"]')
  const end = rules.locator('input[name="notificationRules[0].end"]')
  await expect(start).toBeDisabled()

  await selectZone(page, 'Asia/Kolkata')

  // The seeded window is midnight, and it must still read as midnight after a
  // zone is chosen -- otherwise the default silently shifts by the browser's
  // offset from the alerting zone. This failed intermittently before the form
  // re-anchored the rules on a zone change, so treat any flake here as real.
  await expect(start).toBeEnabled()
  await expect(start).toHaveValue('00:00')

  await start.fill('09:00')
  await end.fill('17:00')

  // Changing the zone must keep the wall-clock times the user just entered.
  // Without re-anchoring, these would shift by the offset between the zones.
  await selectZone(page, 'America/Chicago')
  await expect(start).toHaveValue('09:00')
  await expect(end).toHaveValue('17:00')

  // A second window can be added, and then removed again. Remove the one that
  // was just added -- removing the first would discard the times set above.
  await page.getByRole('button', { name: 'Add alerting window' }).click()
  await expect(
    page.getByRole('button', { name: 'Delete alerting window' }),
  ).toHaveCount(2)
  await page
    .getByRole('button', { name: 'Delete alerting window' })
    .last()
    .click()
  await expect(
    page.getByRole('button', { name: 'Delete alerting window' }),
  ).toHaveCount(0)

  // The seeded window is Mon-Fri; turn Saturday on so the saved weekday filter
  // differs from both the default and an all-true value. The narrow layout
  // swaps the checkboxes for a multi-select, so only assert this when wide --
  // guarding on visibility instead would let the assertion vanish silently.
  //
  // Located by name attribute: these checkboxes carry no visible label of their
  // own (the weekday is a column header), matching ScheduleRuleForm.
  if (!isMobile) {
    await rules.locator('input[name="Saturday"]').check()
  }

  await page.click('[role=dialog] button[type=submit]')
  await expect(page.getByRole('heading', { name: 'Edit Service' })).toBeHidden()

  // Reopening shows what was saved rather than the defaults -- this is what
  // catches a weekday filter that marshals back in the wrong order.
  await page.getByRole('button', { name: 'Edit', exact: true }).click()
  const reopened = page.locator('[data-cy=alert-schedule-rules]')
  await expect(reopened).toBeVisible()
  await expect(
    page.getByRole('combobox', { name: /Alerting Time Zone/ }),
  ).toHaveValue('America/Chicago')

  // The times must come back exactly as they were entered. The form edits ISO
  // instants while the API stores wall-clock times, so a zone-anchoring mistake
  // shows up here as a window shifted by the UTC offset.
  await expect(
    reopened.locator('input[name="notificationRules[0].start"]'),
  ).toHaveValue('09:00')
  await expect(
    reopened.locator('input[name="notificationRules[0].end"]'),
  ).toHaveValue('17:00')

  if (!isMobile) {
    await expect(reopened.locator('input[name="Saturday"]')).toBeChecked()
    await expect(reopened.locator('input[name="Sunday"]')).not.toBeChecked()
    await expect(reopened.locator('input[name="Monday"]')).toBeChecked()
  }
})
