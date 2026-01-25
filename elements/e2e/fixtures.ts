import { test as base, expect, Page } from '@playwright/test'

/**
 * Custom Playwright fixtures for Storybook chat testing.
 *
 * All tests use MSW mocked responses - NO real LLM API calls are made.
 * This ensures tests are fast, deterministic, and cost-free.
 */

export interface ChatTestHelpers {
  /**
   * Navigate to a specific Storybook story
   */
  gotoStory: (storyPath: string) => Promise<void>

  /**
   * Type a message in the chat composer and send it
   */
  sendMessage: (message: string) => Promise<void>

  /**
   * Click a welcome suggestion button
   */
  clickSuggestion: (suggestionText: string) => Promise<void>

  /**
   * Wait for an assistant response to appear
   */
  waitForResponse: (options?: { timeout?: number }) => Promise<void>

  /**
   * Wait for a tool call to appear (pending state)
   */
  waitForToolCall: (toolName?: string, options?: { timeout?: number }) => Promise<void>

  /**
   * Wait for a tool call to complete
   */
  waitForToolComplete: (options?: { timeout?: number }) => Promise<void>

  /**
   * Wait for a chart to render
   */
  waitForChart: (options?: { timeout?: number }) => Promise<void>

  /**
   * Get the chat composer textarea
   */
  getComposer: () => ReturnType<Page['locator']>

  /**
   * Get the send button
   */
  getSendButton: () => ReturnType<Page['locator']>

  /**
   * Get all message elements
   */
  getMessages: () => ReturnType<Page['locator']>

  /**
   * Type @ and wait for tool mention autocomplete
   */
  triggerToolMention: (searchText?: string) => Promise<void>

  /**
   * Get the tool mention autocomplete dropdown
   */
  getToolMentionDropdown: () => ReturnType<Page['locator']>

  /**
   * Click approve button for tool approval
   */
  approveTool: () => Promise<void>

  /**
   * Click reject button for tool approval
   */
  rejectTool: () => Promise<void>
}

export const test = base.extend<{ chat: ChatTestHelpers }>({
  chat: async ({ page }, use) => {
    const helpers: ChatTestHelpers = {
      gotoStory: async (storyPath: string) => {
        // Storybook URL format: /iframe.html?id=<story-id>
        const storyId = storyPath
          .toLowerCase()
          .replace(/\//g, '-')
          .replace(/\s+/g, '-')
        await page.goto(`/iframe.html?id=${storyId}&viewMode=story`)
        // Wait for the chat component to be ready
        await page.waitForSelector('[data-testid="chat-container"], .aui-root', {
          timeout: 10000,
        })
      },

      sendMessage: async (message: string) => {
        // Find the composer textarea - try multiple selectors
        const composer = page.locator(
          'textarea[placeholder*="Message"], textarea[data-testid="composer"], .aui-composer-input textarea, textarea'
        ).first()
        await composer.waitFor({ state: 'visible', timeout: 5000 })
        await composer.fill(message)

        // Find and click send button
        const sendButton = page.locator(
          'button[type="submit"], button[aria-label*="Send"], button[data-testid="send"], .aui-composer-send'
        ).first()
        await sendButton.click()
      },

      clickSuggestion: async (suggestionText: string) => {
        const suggestion = page.locator(`button, [role="button"]`).filter({
          hasText: suggestionText,
        }).first()
        await suggestion.waitFor({ state: 'visible', timeout: 5000 })
        await suggestion.click()
      },

      waitForResponse: async (options = {}) => {
        const timeout = options.timeout ?? 10000
        // Wait for assistant message to appear
        await page.waitForSelector(
          '[data-role="assistant"], [data-message-role="assistant"], .aui-assistant-message',
          { timeout }
        )
      },

      waitForToolCall: async (toolName?: string, options = {}) => {
        const timeout = options.timeout ?? 10000
        const selector = toolName
          ? `[data-tool-name="${toolName}"], [data-testid="tool-call"]:has-text("${toolName}")`
          : '[data-testid="tool-call"], [data-tool-status], .aui-tool-call'
        await page.waitForSelector(selector, { timeout })
      },

      waitForToolComplete: async (options = {}) => {
        const timeout = options.timeout ?? 10000
        await page.waitForSelector(
          '[data-tool-status="complete"], [data-tool-status="completed"], .aui-tool-complete',
          { timeout }
        )
      },

      waitForChart: async (options = {}) => {
        const timeout = options.timeout ?? 15000
        // Vega charts render as SVG
        await page.waitForSelector('svg.marks, [data-testid="chart"] svg', {
          timeout,
        })
      },

      getComposer: () => {
        return page.locator(
          'textarea[placeholder*="Message"], textarea[data-testid="composer"], .aui-composer-input textarea, textarea'
        ).first()
      },

      getSendButton: () => {
        return page.locator(
          'button[type="submit"], button[aria-label*="Send"], button[data-testid="send"], .aui-composer-send'
        ).first()
      },

      getMessages: () => {
        return page.locator(
          '[data-role="user"], [data-role="assistant"], [data-message-role], .aui-message'
        )
      },

      triggerToolMention: async (searchText = '') => {
        const composer = helpers.getComposer()
        await composer.waitFor({ state: 'visible' })
        await composer.fill(`@${searchText}`)
        // Wait a bit for autocomplete to trigger
        await page.waitForTimeout(300)
      },

      getToolMentionDropdown: () => {
        return page.locator(
          '[data-testid="tool-mention-dropdown"], [role="listbox"], .aui-tool-mentions'
        )
      },

      approveTool: async () => {
        const approveButton = page.locator(
          'button:has-text("Approve"), button:has-text("Allow"), button[data-testid="approve-tool"]'
        ).first()
        await approveButton.waitFor({ state: 'visible', timeout: 5000 })
        await approveButton.click()
      },

      rejectTool: async () => {
        const rejectButton = page.locator(
          'button:has-text("Reject"), button:has-text("Deny"), button[data-testid="reject-tool"]'
        ).first()
        await rejectButton.waitFor({ state: 'visible', timeout: 5000 })
        await rejectButton.click()
      },
    }

    await use(helpers)
  },
})

export { expect }
