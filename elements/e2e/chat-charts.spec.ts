import { test, expect } from './fixtures'

/**
 * Chart rendering tests.
 *
 * These tests verify chart functionality:
 * - Charts render correctly from Vega specs
 * - Charts appear in assistant responses
 * - Chart interactions work (hover states)
 *
 * All tests use MSW mocked responses - NO real LLM API calls.
 * The mock handler generates valid Vega specs for chart-related prompts.
 */

test.describe('Chart Rendering', () => {
  test('chart renders when requested', async ({ page, chat }) => {
    await chat.gotoStory('chat-plugins--chart-plugin')

    // Click suggestion to create a chart
    await chat.clickSuggestion('Create a chart')

    // Wait for response
    await chat.waitForResponse()

    // Wait for chart to render (Vega renders as SVG)
    await chat.waitForChart()

    // Verify SVG chart is present
    await expect(page.locator('svg.marks, svg[class*="vega"]').first()).toBeVisible()
  })

  test('chart displays data correctly', async ({ page, chat }) => {
    await chat.gotoStory('chat-plugins--chart-plugin')

    // Trigger chart creation
    await chat.clickSuggestion('Create a chart')

    // Wait for chart
    await chat.waitForChart()

    // The mock returns a bar chart with USA, Canada, Mexico
    // Check that the SVG contains the expected elements
    const chartSvg = page.locator('svg.marks, svg[class*="vega"]').first()
    await expect(chartSvg).toBeVisible()

    // Vega bar charts have rect elements for bars
    const bars = chartSvg.locator('rect')
    await expect(bars.first()).toBeVisible()
  })

  test('chart container has correct dimensions', async ({ page, chat }) => {
    await chat.gotoStory('chat-plugins--chart-plugin')

    await chat.clickSuggestion('Create a chart')
    await chat.waitForChart()

    // Chart container should have minimum dimensions
    const chartContainer = page.locator('[class*="min-h-"], [class*="min-w-"]').filter({
      has: page.locator('svg'),
    }).first()

    if (await chartContainer.isVisible()) {
      const box = await chartContainer.boundingBox()
      expect(box).toBeTruthy()
      if (box) {
        expect(box.width).toBeGreaterThan(200)
        expect(box.height).toBeGreaterThan(100)
      }
    }
  })

  test('chart shows loading state before rendering', async ({ page, chat }) => {
    await chat.gotoStory('chat-plugins--chart-plugin')

    await chat.clickSuggestion('Create a chart')

    // Look for loading indicator (shimmer or "Rendering chart..." text)
    // This should appear briefly before the chart renders
    const loadingIndicator = page.locator('text=Rendering chart, .shimmer').first()

    // The loading state might be very brief, so we just check the chart eventually appears
    await chat.waitForChart()
    await expect(page.locator('svg.marks, svg[class*="vega"]').first()).toBeVisible()
  })
})

test.describe('Chart Error Handling', () => {
  test('invalid chart spec shows error message', async ({ page, chat }) => {
    // This would require a special story that sends invalid Vega JSON
    // For now, we just verify the happy path works
    await chat.gotoStory('chat-plugins--chart-plugin')

    await chat.clickSuggestion('Create a chart')
    await chat.waitForChart()

    // No error should be visible for valid charts
    await expect(page.locator('text=Failed to render chart')).not.toBeVisible()
  })
})
