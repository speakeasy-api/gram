# Playwright E2E Tests for Gram Elements

This directory contains end-to-end tests for the Gram Elements chat components using Playwright + Storybook.

## Zero LLM Cost Testing

**All tests use mocked LLM responses via MSW (Mock Service Worker).** No real API calls are made to OpenRouter, Anthropic, or any other LLM provider. This means:

- Tests are **fast** (no network latency)
- Tests are **deterministic** (same inputs = same outputs)
- Tests are **free** (no API costs)
- Tests can run in **CI** without API keys

## How It Works

1. **Storybook + MSW**: When tests run, Storybook starts with `STORYBOOK_CHROMATIC=true`, which enables MSW handlers.

2. **Smart Mock Responses**: The MSW handlers in `.storybook/mocks/handlers.ts` analyze the user's prompt and return appropriate mock responses:
   - Chart-related prompts → Returns a valid Vega chart spec
   - Tool-related prompts → Returns tool call responses
   - Default → Returns a simple text response

3. **Test Fixtures**: Custom Playwright fixtures in `fixtures.ts` provide helper methods for common chat interactions.

## Running Tests

```bash
# Run all e2e tests
pnpm test:e2e

# Run with UI (interactive mode)
pnpm test:e2e:ui

# Run with browser visible
pnpm test:e2e:headed

# Debug mode
pnpm test:e2e:debug
```

## Test Files

| File | Description |
|------|-------------|
| `chat-basic.spec.ts` | Basic chat interactions, welcome screen, variants |
| `chat-tools.spec.ts` | Tool calling, frontend tools, tool approval |
| `chat-charts.spec.ts` | Chart rendering with Vega |
| `chat-tool-mentions.spec.ts` | @mention autocomplete functionality |
| `fixtures.ts` | Shared test utilities and helpers |

## Adding New Tests

### 1. For new mock response types

Edit `.storybook/mocks/handlers.ts` and add a new condition in `analyzePromptForResponse()`:

```typescript
if (lastUserMessage.includes('my-keyword')) {
  return {
    content: 'My custom response',
    // or for tool calls:
    toolCalls: [{
      id: `call_${Date.now()}`,
      name: 'my_tool',
      arguments: { foo: 'bar' }
    }]
  }
}
```

### 2. For new test scenarios

Create a new spec file or add tests to existing ones:

```typescript
import { test, expect } from './fixtures'

test.describe('My Feature', () => {
  test('does something', async ({ page, chat }) => {
    await chat.gotoStory('chat-my-story--variant')
    await chat.sendMessage('Hello')
    await chat.waitForResponse()
    // assertions...
  })
})
```

### 3. Using the fixtures

The `chat` fixture provides these helpers:

- `gotoStory(path)` - Navigate to a Storybook story
- `sendMessage(text)` - Type and send a message
- `clickSuggestion(text)` - Click a welcome suggestion
- `waitForResponse()` - Wait for assistant response
- `waitForToolCall()` - Wait for tool call UI
- `waitForChart()` - Wait for Vega chart to render
- `triggerToolMention()` - Type @ for autocomplete
- `approveTool()` / `rejectTool()` - Handle tool approval

## Story Path Format

Story paths follow the pattern: `category-subcategory--story-name`

Examples:
- `chat-welcome--custom-message`
- `chat-tools--custom-tool-component`
- `chat-plugins--chart-plugin`
- `chat-toolmentions--default`

## CI Integration

Tests run automatically in CI. The Playwright config:
- Uses single worker in CI for stability
- Retries failed tests twice
- Captures screenshots on failure
- Starts Storybook automatically

## Debugging Tips

1. **View failing screenshots**: Check `test-results/` directory
2. **Use headed mode**: `pnpm test:e2e:headed` to see the browser
3. **Use debug mode**: `pnpm test:e2e:debug` for step-by-step execution
4. **Check Storybook**: Run `pnpm storybook` separately and test stories manually
