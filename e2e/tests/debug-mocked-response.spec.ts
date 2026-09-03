import { test, expect } from '@playwright/test'
import { routes, loginPage, setupPatientE2E } from '../helpers'

/**
 * Debug test to see what happens with mocked responses
 */
test('DEBUG-mocked-response-behavior', async ({ page }) => {
  await setupPatientE2E(page, { withLogin: false })
  await page.goto(routes.login)
  
  const el = loginPage(page)
  
  // Test 1: What does a 500 response look like?
  console.log('=== TEST 1: Status 500 ===')
  await page.route('/api/v1/patient/wx-login', async (route) => {
    console.log('Intercepting 500 request')
    const res = await route.fetch() // Try fetching actual network response
    console.log('Response status:', res.status())
    console.log('Response headers:', res.headers())
    try {
      const jsonData = await res.json()
      console.log('Response JSON:', jsonData)
    } catch (e) {
      console.log('No JSON body, text:', await res.text())
    }
    return route.fulfill({ 
      status: 500, 
      headers: { 'Content-Type': 'application/json' },
      json: { message: 'Internal Server Error' }
    })
  })
  
  // Click login button
  console.log('Clicking wechat login button...')
  await el.wechatBtn.click()
  await page.waitForTimeout(3000)
  
  console.log('Current URL after click:', page.url())
  
  const hasToast = await page.locator('uni-toast').count() > 0
  console.log('Has uni-toast element:', hasToast)
  
  const hasCustomToast = await page.locator('.toast').count() > 0
  console.log('Has custom toast element:', hasCustomToast)
})
