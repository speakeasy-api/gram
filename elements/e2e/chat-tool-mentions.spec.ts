import { test, expect } from './fixtures'

/**
 * Tool mentions autocomplete tests.
 *
 * These tests verify the @mention autocomplete functionality:
 * - Typing @ shows autocomplete dropdown
 * - Filtering works as you type
 * - Selecting a tool adds it to the message
 * - Keyboard navigation works
 * - Tool mentions can be disabled via config
 *
 * All tests use MSW mocked responses - NO real LLM API calls.
 */

test.describe('Tool Mentions Autocomplete', () => {
  test('typing @ shows autocomplete dropdown', async ({ page, chat }) => {
    await chat.gotoStory('chat-toolmentions--default')

    // Type @ to trigger autocomplete
    await chat.triggerToolMention()

    // Wait for dropdown to appear
    const dropdown = chat.getToolMentionDropdown()
    await expect(dropdown).toBeVisible({ timeout: 5000 })
  })

  test('autocomplete filters tools as you type', async ({ page, chat }) => {
    await chat.gotoStory('chat-toolmentions--default')

    // Type @sal to filter for salutation
    await chat.triggerToolMention('sal')

    // Dropdown should show filtered results
    const dropdown = chat.getToolMentionDropdown()
    await expect(dropdown).toBeVisible({ timeout: 5000 })

    // Should show the salutation tool
    await expect(
      page.locator('[role="option"], [role="listbox"] li, .aui-tool-mention-option').filter({
        hasText: /salutation/i,
      })
    ).toBeVisible()
  })

  test('selecting a tool from dropdown adds mention', async ({ page, chat }) => {
    await chat.gotoStory('chat-toolmentions--default')

    // Trigger autocomplete
    await chat.triggerToolMention()

    // Wait for dropdown
    await page.waitForSelector('[role="option"], [role="listbox"] li', { timeout: 5000 })

    // Click first option
    await page.locator('[role="option"], [role="listbox"] li').first().click()

    // Verify composer contains the mention
    const composer = chat.getComposer()
    const value = await composer.inputValue()
    expect(value).toContain('@')
  })

  test('keyboard navigation works in autocomplete', async ({ page, chat }) => {
    await chat.gotoStory('chat-toolmentions--default')

    // Trigger autocomplete
    await chat.triggerToolMention()

    // Wait for dropdown
    await page.waitForSelector('[role="option"], [role="listbox"] li', { timeout: 5000 })

    // Press down arrow to navigate
    await page.keyboard.press('ArrowDown')

    // Press Enter to select
    await page.keyboard.press('Enter')

    // Verify something was selected
    const composer = chat.getComposer()
    const value = await composer.inputValue()
    expect(value.length).toBeGreaterThan(1) // More than just "@"
  })

  test('escape closes the autocomplete dropdown', async ({ page, chat }) => {
    await chat.gotoStory('chat-toolmentions--default')

    // Trigger autocomplete
    await chat.triggerToolMention()

    // Wait for dropdown to appear
    const dropdown = chat.getToolMentionDropdown()
    await expect(dropdown).toBeVisible({ timeout: 5000 })

    // Press Escape
    await page.keyboard.press('Escape')

    // Dropdown should be hidden
    await expect(dropdown).not.toBeVisible({ timeout: 2000 })
  })
})

test.describe('Tool Mentions Configuration', () => {
  test('tool mentions can be disabled', async ({ page, chat }) => {
    await chat.gotoStory('chat-toolmentions--disabled')

    // Type @ - should NOT trigger autocomplete
    await chat.triggerToolMention()

    // Wait a bit to ensure no dropdown appears
    await page.waitForTimeout(500)

    // Dropdown should not be visible
    const dropdown = chat.getToolMentionDropdown()
    await expect(dropdown).not.toBeVisible()
  })

  test('custom config limits suggestions', async ({ page, chat }) => {
    await chat.gotoStory('chat-toolmentions--custom-config')

    // Trigger autocomplete
    await chat.triggerToolMention()

    // Wait for dropdown
    await page.waitForSelector('[role="option"], [role="listbox"] li', { timeout: 5000 })

    // Count visible options - should be limited by maxSuggestions (5 in this story)
    const options = page.locator('[role="option"], [role="listbox"] li')
    const count = await options.count()
    expect(count).toBeLessThanOrEqual(5)
  })
})

test.describe('Tool Mention Display', () => {
  test('mentioned tools show as badges after selection', async ({ page, chat }) => {
    await chat.gotoStory('chat-toolmentions--default')

    // Select a tool
    await chat.triggerToolMention()
    await page.waitForSelector('[role="option"], [role="listbox"] li', { timeout: 5000 })
    await page.locator('[role="option"], [role="listbox"] li').first().click()

    // Look for tool badge/pill UI
    await expect(
      page.locator('[data-testid="mentioned-tool"], .aui-mentioned-tool, [class*="badge"]').first()
    ).toBeVisible({ timeout: 3000 })
  })
})
