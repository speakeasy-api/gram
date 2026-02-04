/* eslint-disable @typescript-eslint/no-explicit-any */
import { describe, expect, it } from 'vitest'
import {
  convertGramMessagesToUIMessages,
  convertGramMessagePartsToUIMessageParts,
  type GramChatMessage,
} from './messageConverter'

/**
 * Helper to create a minimal GramChatMessage for testing.
 */
function makeMsg(
  overrides: Partial<GramChatMessage> & { role: string }
): GramChatMessage {
  return {
    id: crypto.randomUUID(),
    model: 'test-model',
    created_at: new Date().toISOString(),
    ...overrides,
  } as GramChatMessage
}

function makeToolCallsJSON(
  calls: { id: string; name: string; args?: string }[]
): string {
  return JSON.stringify(
    calls.map((c) => ({
      id: c.id,
      type: 'function',
      function: { name: c.name, arguments: c.args ?? '{}' },
    }))
  )
}

describe('convertGramMessagePartsToUIMessageParts', () => {
  it('includes tool calls for a single assistant message', () => {
    const msg = makeMsg({
      role: 'assistant',
      content: 'Let me search.',
      tool_calls: makeToolCallsJSON([{ id: 'tc_1', name: 'search_deals' }]),
    } as Partial<GramChatMessage> & { role: string })

    const parts = convertGramMessagePartsToUIMessageParts(msg as any, new Map())

    const toolParts = parts.filter((p) => p.type === 'dynamic-tool')
    expect(toolParts).toHaveLength(1)
    expect(toolParts[0]).toMatchObject({
      type: 'dynamic-tool',
      toolCallId: 'tc_1',
      toolName: 'search_deals',
    })
  })

  it('deduplicates tool calls when seenToolCallIds is provided', () => {
    const seen = new Set(['tc_1'])
    const msg = makeMsg({
      role: 'assistant',
      content: 'Trying again.',
      tool_calls: makeToolCallsJSON([
        { id: 'tc_1', name: 'search_deals' },
        { id: 'tc_2', name: 'search_deals' },
      ]),
    } as Partial<GramChatMessage> & { role: string })

    const parts = convertGramMessagePartsToUIMessageParts(
      msg as any,
      new Map(),
      seen
    )

    const toolParts = parts.filter((p) => p.type === 'dynamic-tool')
    expect(toolParts).toHaveLength(1)
    expect(toolParts[0]).toMatchObject({ toolCallId: 'tc_2' })
    expect(seen.has('tc_2')).toBe(true)
  })
})

describe('convertGramMessagesToUIMessages - tool call deduplication', () => {
  /**
   * Simulates the server behavior where each assistant message in a multi-step
   * tool use flow accumulates ALL tool calls from the turn, not just its own.
   *
   * Given 4 sequential tool calls, the server stores:
   *   message 1: tool_calls = [tc_1]
   *   message 2: tool_calls = [tc_1, tc_2]
   *   message 3: tool_calls = [tc_1, tc_2, tc_3]
   *   message 4: tool_calls = [tc_1, tc_2, tc_3, tc_4]
   *
   * Without dedup, each message renders all its tool calls → every group shows
   * the accumulated count. With dedup, each message only renders the new ones.
   */
  it('deduplicates accumulated tool calls across assistant messages', () => {
    const messages: GramChatMessage[] = [
      makeMsg({
        role: 'user',
        content: 'Search for deals',
      }),
      makeMsg({
        role: 'assistant',
        content: "I'll search for deals.",
        tool_calls: makeToolCallsJSON([
          { id: 'tc_1', name: 'hubspot_search_deals' },
        ]),
      } as Partial<GramChatMessage> & { role: string }),
      makeMsg({
        role: 'tool',
        content: '{"error": "invalid filter"}',
        tool_call_id: 'tc_1',
      } as Partial<GramChatMessage> & { role: string }),
      makeMsg({
        role: 'assistant',
        content: 'Let me try differently.',
        tool_calls: makeToolCallsJSON([
          { id: 'tc_1', name: 'hubspot_search_deals' },
          { id: 'tc_2', name: 'hubspot_search_deals' },
        ]),
      } as Partial<GramChatMessage> & { role: string }),
      makeMsg({
        role: 'tool',
        content: '{"error": "empty filters"}',
        tool_call_id: 'tc_2',
      } as Partial<GramChatMessage> & { role: string }),
      makeMsg({
        role: 'assistant',
        content: 'Let me try with proper filters.',
        tool_calls: makeToolCallsJSON([
          { id: 'tc_1', name: 'hubspot_search_deals' },
          { id: 'tc_2', name: 'hubspot_search_deals' },
          { id: 'tc_3', name: 'hubspot_search_deals' },
        ]),
      } as Partial<GramChatMessage> & { role: string }),
      makeMsg({
        role: 'tool',
        content: '{"deals": []}',
        tool_call_id: 'tc_3',
      } as Partial<GramChatMessage> & { role: string }),
      makeMsg({
        role: 'assistant',
        content: 'Here are the results.',
      }),
    ]

    const result = convertGramMessagesToUIMessages(messages)
    const assistantMessages = result.messages.filter(
      (m) => m.message.role === 'assistant'
    )

    // Each assistant message should only have its OWN tool call, not all accumulated ones
    const firstAssistant = assistantMessages[0]!.message.parts.filter(
      (p) => p.type === 'dynamic-tool'
    )
    expect(firstAssistant).toHaveLength(1)
    expect(firstAssistant[0]).toMatchObject({ toolCallId: 'tc_1' })

    const secondAssistant = assistantMessages[1]!.message.parts.filter(
      (p) => p.type === 'dynamic-tool'
    )
    expect(secondAssistant).toHaveLength(1)
    expect(secondAssistant[0]).toMatchObject({ toolCallId: 'tc_2' })

    const thirdAssistant = assistantMessages[2]!.message.parts.filter(
      (p) => p.type === 'dynamic-tool'
    )
    expect(thirdAssistant).toHaveLength(1)
    expect(thirdAssistant[0]).toMatchObject({ toolCallId: 'tc_3' })

    // Final assistant message has no tool calls
    const fourthAssistant = assistantMessages[3]!.message.parts.filter(
      (p) => p.type === 'dynamic-tool'
    )
    expect(fourthAssistant).toHaveLength(0)
  })

  it('resets dedup tracking after a user message', () => {
    const messages: GramChatMessage[] = [
      makeMsg({ role: 'user', content: 'First question' }),
      makeMsg({
        role: 'assistant',
        content: 'Searching.',
        tool_calls: makeToolCallsJSON([{ id: 'tc_1', name: 'search' }]),
      } as Partial<GramChatMessage> & { role: string }),
      makeMsg({
        role: 'tool',
        content: '{}',
        tool_call_id: 'tc_1',
      } as Partial<GramChatMessage> & { role: string }),
      makeMsg({ role: 'user', content: 'Second question' }),
      // New turn — tc_1 reused as ID (different conversation turn)
      makeMsg({
        role: 'assistant',
        content: 'Searching again.',
        tool_calls: makeToolCallsJSON([{ id: 'tc_1', name: 'search' }]),
      } as Partial<GramChatMessage> & { role: string }),
    ]

    const result = convertGramMessagesToUIMessages(messages)
    const assistantMessages = result.messages.filter(
      (m) => m.message.role === 'assistant'
    )

    // Both assistant messages should have tc_1 since the user message resets tracking
    const first = assistantMessages[0]!.message.parts.filter(
      (p) => p.type === 'dynamic-tool'
    )
    expect(first).toHaveLength(1)

    const second = assistantMessages[1]!.message.parts.filter(
      (p) => p.type === 'dynamic-tool'
    )
    expect(second).toHaveLength(1)
  })
})
