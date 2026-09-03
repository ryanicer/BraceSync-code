import { test, expect } from '@playwright/test'
import { routes, loginPage, setupPatientE2E } from '../helpers'

/**
 * Debug test to reproduce L3/L6 failure where 500 response causes navigation
 */
test('DEBUG-500-response-triggers-navigation', async ({ page }) => {
  await setupPatientE2E(page, { withLogin: false })
  await page.goto(routes.login)
  
  const el = loginPage(page)
  
  // Track URL changes
  let currentUrl = ''
  page.on('url', (url) => {
    currentUrl = url
    console.log('URL changed:', url)
  })
  
  // Mock 500 response
  let requestCalled = false
  await page.route('/api/v1/patient/wx-login', async (route) => {
    requestCalled = true
    console.log('Intercepting wx-login request, returning 500...')
    return route.fulfill({ status: 500, json: { message: 'Internal Server Error' } })
  })
  
  // Click login button
  console.log('Clicking wechat login button...')
  await el.wechatBtn.click()
  
  // Wait for potential navigation
  await page.waitForTimeout(3000)
  
  const finalUrl = page.url()
  console.log('Final URL:', finalUrl)
  console.log('Request was called:', requestCalled)
  
  // Check if we're still on login page
  await expect(page).toHaveURL(/pages\/login/, { timeout: 1000 })
})
