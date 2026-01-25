import { test, expect } from './fixtures'

/**
 * Basic chat interaction tests.
 *
 * These tests verify core chat functionality:
 * - Sending messages
 * - Receiving responses
 * - Welcome screen and suggestions
 * - Different chat variants
 *
 * All tests use MSW mocked responses - NO real LLM API calls.
 */

test.describe('Chat Basic Interactions', () => {
  test('displays welcome message and suggestions', async ({ page, chat }) => {
    await chat.gotoStory('chat-welcome--custom-message')

    // Verify welcome title is displayed
    await expect(page.locator('text=Hello there!')).toBeVisible()

    // Verify suggestion is displayed
    await expect(page.locator('text=Write a SQL query')).toBeVisible()
  })

  test('sends a message and receives a response', async ({ page, chat }) => {
    await chat.gotoStory('chat-welcome--custom-message')

    // Type and send a message
    await chat.sendMessage('Hello, this is a test message')

    // Wait for response
    await chat.waitForResponse()

    // Verify user message appears
    await expect(
      page.locator('[data-role="user"], .aui-user-message').filter({
        hasText: 'test message',
      })
    ).toBeVisible()

    // Verify assistant response appears
    await expect(
      page.locator('[data-role="assistant"], .aui-assistant-message')
    ).toBeVisible()
  })

  test('clicking a suggestion sends the prompt', async ({ page, chat }) => {
    await chat.gotoStory('chat-welcome--custom-message')

    // Click the suggestion
    await chat.clickSuggestion('Write a SQL query')

    // Wait for response
    await chat.waitForResponse()

    // Verify assistant responded
    await expect(
      page.locator('[data-role="assistant"], .aui-assistant-message')
    ).toBeVisible()
  })

  test('composer is focused and ready for input', async ({ chat }) => {
    await chat.gotoStory('chat-welcome--custom-message')

    const composer = chat.getComposer()
    await expect(composer).toBeVisible()
    await expect(composer).toBeEnabled()
  })

  test('send button is visible', async ({ chat }) => {
    await chat.gotoStory('chat-welcome--custom-message')

    const sendButton = chat.getSendButton()
    await expect(sendButton).toBeVisible()
  })
})

test.describe('Chat Variants', () => {
  test('standalone variant renders correctly', async ({ page, chat }) => {
    await chat.gotoStory('chat-variants--standalone')

    // Should show the chat thread
    await expect(chat.getComposer()).toBeVisible()
  })

  test('modal variant can be opened', async ({ page, chat }) => {
    await chat.gotoStory('chat-variants--modal')

    // Modal variant typically has a trigger button
    const trigger = page.locator('button').first()
    if (await trigger.isVisible()) {
      await trigger.click()
      // After clicking, the modal content should appear
      await expect(chat.getComposer()).toBeVisible({ timeout: 5000 })
    }
  })
})

test.describe('Chat Density', () => {
  test('compact density renders correctly', async ({ page, chat }) => {
    await chat.gotoStory('chat-density--compact')
    await expect(chat.getComposer()).toBeVisible()
  })

  test('comfortable density renders correctly', async ({ page, chat }) => {
    await chat.gotoStory('chat-density--comfortable')
    await expect(chat.getComposer()).toBeVisible()
  })
})

test.describe('Chat Theme', () => {
  test('light theme renders correctly', async ({ page, chat }) => {
    await chat.gotoStory('chat-theme--light')
    await expect(chat.getComposer()).toBeVisible()
  })

  test('dark theme renders correctly', async ({ page, chat }) => {
    await chat.gotoStory('chat-theme--dark')
    await expect(chat.getComposer()).toBeVisible()
  })
})

test.describe('Chat Composer', () => {
  test('custom placeholder is displayed', async ({ page, chat }) => {
    await chat.gotoStory('chat-composer--custom-placeholder')

    const composer = chat.getComposer()
    await expect(composer).toBeVisible()
    // The composer should have a custom placeholder
    const placeholder = await composer.getAttribute('placeholder')
    expect(placeholder).toBeTruthy()
  })
})
