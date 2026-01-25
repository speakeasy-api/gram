import { test, expect } from './fixtures'

/**
 * Tool calling tests.
 *
 * These tests verify tool execution UX:
 * - Tool calls are displayed
 * - Tool results are rendered
 * - Custom tool components work
 * - Frontend tools execute correctly
 *
 * All tests use MSW mocked responses - NO real LLM API calls.
 */

test.describe('Tool Calling', () => {
  test('displays tool call when triggered', async ({ page, chat }) => {
    await chat.gotoStory('chat-tools--custom-tool-component')

    // Click suggestion that triggers a tool call
    await chat.clickSuggestion('Get card details')

    // Wait for tool call to appear
    await chat.waitForToolCall()

    // Verify tool UI is displayed
    await expect(
      page.locator('[data-testid="tool-call"], .aui-tool-call, [data-tool-name]').first()
    ).toBeVisible({ timeout: 10000 })
  })

  test('custom tool component renders correctly', async ({ page, chat }) => {
    await chat.gotoStory('chat-tools--custom-tool-component')

    // Trigger the card details tool
    await chat.clickSuggestion('Get card details')

    // Wait for response
    await chat.waitForResponse()

    // The custom CardPinRevealComponent should render
    // Look for card-related UI elements
    await expect(
      page.locator('text=VISA, text=Click to flip, [data-testid="card-component"]').first()
    ).toBeVisible({ timeout: 15000 })
  })
})

test.describe('Frontend Tools', () => {
  test('frontend tool executes and displays result', async ({ page, chat }) => {
    await chat.gotoStory('chat-frontend-tools--fetch-url')

    // Click the fetch suggestion
    await chat.clickSuggestion('Fetch a URL')

    // Wait for tool execution
    await chat.waitForToolCall()

    // The FetchToolComponent renders a browser-like UI
    await expect(
      page.locator('text=httpbin.org, .bg-red-500, .bg-yellow-500, .bg-green-500').first()
    ).toBeVisible({ timeout: 15000 })
  })
})

test.describe('Tool Approval', () => {
  test('tool requiring approval shows approval UI', async ({ page, chat }) => {
    await chat.gotoStory('chat-tool-approval--single-tool')

    // Click suggestion that triggers tool requiring approval
    await chat.clickSuggestion('Get a salutation')

    // Wait for the approval UI to appear
    await expect(
      page.locator('button:has-text("Approve"), button:has-text("Allow"), button:has-text("Run")').first()
    ).toBeVisible({ timeout: 10000 })
  })

  test('approving tool executes it', async ({ page, chat }) => {
    await chat.gotoStory('chat-tool-approval--single-tool')

    // Trigger tool
    await chat.clickSuggestion('Get a salutation')

    // Approve the tool
    await chat.approveTool()

    // Wait for response after approval
    await chat.waitForResponse({ timeout: 15000 })
  })

  test('multiple grouped tools show combined approval UI', async ({ page, chat }) => {
    await chat.gotoStory('chat-tool-approval--multiple-grouped-tools')

    // Trigger multiple tools
    await chat.clickSuggestion('Call both tools')

    // Should show approval UI for multiple tools
    await expect(
      page.locator('button:has-text("Approve"), button:has-text("Allow")').first()
    ).toBeVisible({ timeout: 10000 })
  })

  test('frontend tool requiring approval works', async ({ page, chat }) => {
    await chat.gotoStory('chat-tool-approval--frontend-tool')

    // Trigger delete file tool
    await chat.clickSuggestion('Delete a file')

    // Should show approval UI
    await expect(
      page.locator('button:has-text("Approve"), button:has-text("Allow")').first()
    ).toBeVisible({ timeout: 10000 })
  })
})

test.describe('Tool States', () => {
  test('tool shows pending state before execution', async ({ page, chat }) => {
    await chat.gotoStory('chat-tools--custom-tool-component')

    // Send a message that triggers a tool
    await chat.clickSuggestion('Get card details')

    // Look for pending/loading indicator
    // This might be a spinner, "Running..." text, or similar
    await expect(
      page.locator('[data-tool-status="pending"], [data-tool-status="running"], .aui-tool-pending, text=Running').first()
    ).toBeVisible({ timeout: 5000 })
  })
})
