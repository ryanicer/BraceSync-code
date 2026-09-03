import { test, expect } from '@playwright/test'
import { routes, loginPage, setupPatientE2E } from '../helpers'

/**
 * Debug L3 failure - 500 response should NOT navigate
 */
test('DEBUG-L3-500-response-behavior', async ({ page }) => {
  // Capture all console messages from page
  const consoleMessages: string[] = []
  page.on('console', msg => {
    consoleMessages.push(msg.text())
    console.log('[BROWSER]', msg.type(), msg.text())
  })
  
  await setupPatientE2E(page, { withLogin: false })
  await page.goto(routes.login)
  
  await page.route('/api/v1/patient/wx-login', async (route) => {
    console.log('[TEST] Intercepting route, returning 500')
    return route.fulfill({ status: 500, json: { message: 'Internal Server Error' } })
  })
  
  const el = loginPage(page)
  console.log('[TEST] Clicking login button...')
  await el.wechatBtn.click()
  
  console.log('[TEST] Waiting 5s for potential navigation...')
  await page.waitForTimeout(5000)
  
  console.log('\n=== BROWSER CONSOLE MESSAGES ===')
  consoleMessages.forEach((m, i) => console.log(`[${i}] ${m}`))
  
  console.log('\nCurrent URL:', page.url())
  
  const isOnMonitor = page.url().includes('/pages/monitor/')
  console.log('Is on monitor (SHOULD BE FALSE):', isOnMonitor)
})
