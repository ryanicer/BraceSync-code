import { test, expect } from '@playwright/test'
import { routes, loginPage, setupPatientE2E } from '../helpers'

/**
 * Debug test to see the actual login flow behavior
 */
test('DEBUG-login-success-path', async ({ page }) => {
  // Set up console listener BEFORE any navigation
  const debugLogs: string[] = []
  page.on('console', (msg) => {
    debugLogs.push(`[${msg.type()}] ${msg.text()}`)
    console.log(`[BROWSER-CONSOLE] ${msg.text()}`)
  })
  
  await setupPatientE2E(page, { withLogin: false })
  await page.goto(routes.login)
  
  const el = loginPage(page)
  
  // Track URL changes
  let urlHistory: string[] = []
  page.on('url', (url) => {
    urlHistory.push(url)
    console.log(`URL changed[${urlHistory.length}]:`, url)
  })
  
  let capturedRequestData: any
  let requestCallCount = 0
  
  await page.route('/api/v1/patient/wx-login', async (route) => {
    requestCallCount++
    
    // Capture request data
    try {
      capturedRequestData = route.request().postDataJSON()
    } catch (e) {
      capturedRequestData = null
    }
    
    console.log(`[MOCK] Request ${requestCallCount}:`, capturedRequestData)
    
    // Return success response matching fixture format
    return route.fulfill({ 
      status: 200,
      headers: { 'Content-Type': 'application/json' },
      json: {
        code: 0,
        message: 'ok',
        data: {
          token: 'mock-jwt-token-debug',
          patientId: 'PT-DEBUG-001',
          name: 'Debug User',
          role: 'patient'
        }
      }
    })
  })
  
  console.log('Clicking wechat login button...')
  await el.wechatBtn.click()
  
  console.log('Waiting for potential navigation...')
  await page.waitForTimeout(5000)
  
  console.log('\n=== DEBUG INFO ===')
  console.log('Final URL:', page.url())
  console.log('URL history:', urlHistory)
  console.log('Request count:', requestCallCount)
  console.log('Captured request data:', JSON.stringify(capturedRequestData, null, 2))
  
  const hasToastVisible = await page.locator('.toast').isVisible({ timeout: 1000 })
  console.log('Has visible toast:', hasToastVisible)
  
  if (hasToastVisible) {
    const toastText = await page.locator('.toast-text').first().textContent()
    console.log('Toast text:', toastText)
  }
  
  // If navigation happened, verify it's monitor page
  const isOnMonitor = page.url().includes('/pages/monitor/')
  console.log('Is on monitor page:', isOnMonitor)
  
  console.log('\n=== ALL BROWSER CONSOLE LOGS ===')
  console.log(debugLogs.join('\n'))
})
