import { describe, expect, it } from 'vitest'
import {
  convertGramMessagesToExported,
  type GramChatMessage,
} from './messageConverter'

describe('convertGramMessagesToExported', () => {
  describe('empty and basic cases', () => {
    it('returns empty repository for empty messages array', () => {
      const result = convertGramMessagesToExported([])

      expect(result).toEqual({
        messages: [],
        headId: null,
      })
    })

    it('converts a single user message', () => {
      const messages: GramChatMessage[] = [
        {
          id: 'msg-1',
          role: 'user',
          content: 'Hello world',
          model: 'gpt-4',
          createdAt: '2024-01-15T10:00:00Z',
        },
      ]

      const result = convertGramMessagesToExported(messages)

      expect(result.headId).toBe('msg-1')
      expect(result.messages).toHaveLength(1)
      expect(result.messages[0].message.role).toBe('user')
      expect(result.messages[0].message.id).toBe('msg-1')
      expect(result.messages[0].parentId).toBeNull()
    })

    it('converts a single assistant message', () => {
      const messages: GramChatMessage[] = [
        {
          id: 'msg-1',
          role: 'assistant',
          content: 'Hello! How can I help?',
          model: 'gpt-4',
          createdAt: '2024-01-15T10:00:00Z',
        },
      ]

      const result = convertGramMessagesToExported(messages)

      expect(result.headId).toBe('msg-1')
      expect(result.messages).toHaveLength(1)
      expect(result.messages[0].message.role).toBe('assistant')
    })
  })

  describe('message threading', () => {
    it('creates parent-child relationships for multiple messages', () => {
      const messages: GramChatMessage[] = [
        {
          id: 'msg-1',
          role: 'user',
          content: 'Hello',
          model: 'gpt-4',
          createdAt: '2024-01-15T10:00:00Z',
        },
        {
          id: 'msg-2',
          role: 'assistant',
          content: 'Hi there!',
          model: 'gpt-4',
          createdAt: '2024-01-15T10:00:01Z',
        },
        {
          id: 'msg-3',
          role: 'user',
          content: 'How are you?',
          model: 'gpt-4',
          createdAt: '2024-01-15T10:00:02Z',
        },
      ]

      const result = convertGramMessagesToExported(messages)

      expect(result.headId).toBe('msg-3')
      expect(result.messages).toHaveLength(3)
      expect(result.messages[0].parentId).toBeNull()
      expect(result.messages[1].parentId).toBe('msg-1')
      expect(result.messages[2].parentId).toBe('msg-2')
    })
  })

  describe('system messages', () => {
    it('filters out system messages', () => {
      const messages: GramChatMessage[] = [
        {
          id: 'msg-1',
          role: 'system',
          content: 'You are a helpful assistant',
          model: 'gpt-4',
          createdAt: '2024-01-15T10:00:00Z',
        },
        {
          id: 'msg-2',
          role: 'user',
          content: 'Hello',
          model: 'gpt-4',
          createdAt: '2024-01-15T10:00:01Z',
        },
      ]

      const result = convertGramMessagesToExported(messages)

      expect(result.messages).toHaveLength(1)
      expect(result.messages[0].message.id).toBe('msg-2')
      expect(result.messages[0].parentId).toBeNull()
    })

    it('returns empty when only system messages exist', () => {
      const messages: GramChatMessage[] = [
        {
          id: 'msg-1',
          role: 'system',
          content: 'You are a helpful assistant',
          model: 'gpt-4',
          createdAt: '2024-01-15T10:00:00Z',
        },
      ]

      const result = convertGramMessagesToExported(messages)

      expect(result.messages).toHaveLength(0)
      expect(result.headId).toBeNull()
    })
  })

  describe('user message content', () => {
    it('builds text content part from message content', () => {
      const messages: GramChatMessage[] = [
        {
          id: 'msg-1',
          role: 'user',
          content: 'Test message',
          model: 'gpt-4',
          createdAt: '2024-01-15T10:00:00Z',
        },
      ]

      const result = convertGramMessagesToExported(messages)
      const content = result.messages[0].message.content

      expect(content).toHaveLength(1)
      expect(content[0]).toMatchObject({
        type: 'text',
        text: 'Test message',
      })
    })

    it('creates empty text part when content is missing', () => {
      const messages: GramChatMessage[] = [
        {
          id: 'msg-1',
          role: 'user',
          content: undefined,
          model: 'gpt-4',
          createdAt: '2024-01-15T10:00:00Z',
        },
      ]

      const result = convertGramMessagesToExported(messages)
      const content = result.messages[0].message.content

      expect(content).toHaveLength(1)
      expect(content[0]).toMatchObject({
        type: 'text',
        text: '',
      })
    })

    it('creates empty text part when content is empty string', () => {
      const messages: GramChatMessage[] = [
        {
          id: 'msg-1',
          role: 'user',
          content: '',
          model: 'gpt-4',
          createdAt: '2024-01-15T10:00:00Z',
        },
      ]

      const result = convertGramMessagesToExported(messages)
      const content = result.messages[0].message.content

      expect(content).toHaveLength(1)
      expect(content[0]).toMatchObject({
        type: 'text',
        text: '',
      })
    })
  })

  describe('assistant message content', () => {
    it('builds text content part from message content', () => {
      const messages: GramChatMessage[] = [
        {
          id: 'msg-1',
          role: 'assistant',
          content: 'I can help with that!',
          model: 'gpt-4',
          createdAt: '2024-01-15T10:00:00Z',
        },
      ]

      const result = convertGramMessagesToExported(messages)
      const content = result.messages[0].message.content

      expect(content).toHaveLength(1)
      expect(content[0]).toMatchObject({
        type: 'text',
        text: 'I can help with that!',
      })
    })

    it('creates empty text part when no content or tool calls', () => {
      const messages: GramChatMessage[] = [
        {
          id: 'msg-1',
          role: 'assistant',
          content: undefined,
          model: 'gpt-4',
          createdAt: '2024-01-15T10:00:00Z',
        },
      ]

      const result = convertGramMessagesToExported(messages)
      const content = result.messages[0].message.content

      expect(content).toHaveLength(1)
      expect(content[0]).toMatchObject({
        type: 'text',
        text: '',
      })
    })

    it('sets complete status on assistant messages', () => {
      const messages: GramChatMessage[] = [
        {
          id: 'msg-1',
          role: 'assistant',
          content: 'Done!',
          model: 'gpt-4',
          createdAt: '2024-01-15T10:00:00Z',
        },
      ]

      const result = convertGramMessagesToExported(messages)
      const msg = result.messages[0].message

      expect(msg.role).toBe('assistant')
      if (msg.role === 'assistant') {
        expect(msg.status).toMatchObject({
          type: 'complete',
          reason: 'stop',
        })
      }
    })
  })

  describe('tool calls', () => {
    it('parses tool calls with function format', () => {
      const toolCalls = JSON.stringify([
        {
          id: 'call-1',
          function: {
            name: 'get_weather',
            arguments: JSON.stringify({ city: 'London' }),
          },
        },
      ])

      const messages: GramChatMessage[] = [
        {
          id: 'msg-1',
          role: 'assistant',
          content: undefined,
          model: 'gpt-4',
          toolCalls,
          createdAt: '2024-01-15T10:00:00Z',
        },
      ]

      const result = convertGramMessagesToExported(messages)
      const content = result.messages[0].message.content

      expect(content).toHaveLength(1)
      expect(content[0]).toMatchObject({
        type: 'tool-call',
        toolCallId: 'call-1',
        toolName: 'get_weather',
        args: { city: 'London' },
      })
    })

    it('parses tool calls with direct args format', () => {
      const toolCalls = JSON.stringify([
        {
          toolCallId: 'call-1',
          toolName: 'search',
          args: { query: 'test' },
        },
      ])

      const messages: GramChatMessage[] = [
        {
          id: 'msg-1',
          role: 'assistant',
          content: undefined,
          model: 'gpt-4',
          toolCalls,
          createdAt: '2024-01-15T10:00:00Z',
        },
      ]

      const result = convertGramMessagesToExported(messages)
      const content = result.messages[0].message.content

      expect(content).toHaveLength(1)
      expect(content[0]).toMatchObject({
        type: 'tool-call',
        toolCallId: 'call-1',
        toolName: 'search',
        args: { query: 'test' },
      })
    })

    it('includes both text and tool calls in content', () => {
      const toolCalls = JSON.stringify([
        {
          id: 'call-1',
          function: {
            name: 'calculate',
            arguments: JSON.stringify({ a: 1, b: 2 }),
          },
        },
      ])

      const messages: GramChatMessage[] = [
        {
          id: 'msg-1',
          role: 'assistant',
          content: 'Let me calculate that for you.',
          model: 'gpt-4',
          toolCalls,
          createdAt: '2024-01-15T10:00:00Z',
        },
      ]

      const result = convertGramMessagesToExported(messages)
      const content = result.messages[0].message.content

      expect(content).toHaveLength(2)
      expect(content[0]).toMatchObject({
        type: 'text',
        text: 'Let me calculate that for you.',
      })
      expect(content[1]).toMatchObject({
        type: 'tool-call',
        toolName: 'calculate',
      })
    })

    it('handles multiple tool calls', () => {
      const toolCalls = JSON.stringify([
        {
          id: 'call-1',
          function: {
            name: 'tool_a',
            arguments: '{}',
          },
        },
        {
          id: 'call-2',
          function: {
            name: 'tool_b',
            arguments: '{}',
          },
        },
      ])

      const messages: GramChatMessage[] = [
        {
          id: 'msg-1',
          role: 'assistant',
          content: undefined,
          model: 'gpt-4',
          toolCalls,
          createdAt: '2024-01-15T10:00:00Z',
        },
      ]

      const result = convertGramMessagesToExported(messages)
      const content = result.messages[0].message.content

      expect(content).toHaveLength(2)
      expect(content[0]).toMatchObject({ toolName: 'tool_a' })
      expect(content[1]).toMatchObject({ toolName: 'tool_b' })
    })

    it('handles invalid tool calls JSON gracefully', () => {
      const messages: GramChatMessage[] = [
        {
          id: 'msg-1',
          role: 'assistant',
          content: 'Some text',
          model: 'gpt-4',
          toolCalls: 'not valid json',
          createdAt: '2024-01-15T10:00:00Z',
        },
      ]

      const result = convertGramMessagesToExported(messages)
      const content = result.messages[0].message.content

      // Should still have the text content, tool calls are ignored
      expect(content).toHaveLength(1)
      expect(content[0]).toMatchObject({
        type: 'text',
        text: 'Some text',
      })
    })

    it('handles tool calls with args as object (not stringified)', () => {
      const toolCalls = JSON.stringify([
        {
          id: 'call-1',
          function: {
            name: 'test_tool',
            arguments: { key: 'value' }, // Already an object
          },
        },
      ])

      const messages: GramChatMessage[] = [
        {
          id: 'msg-1',
          role: 'assistant',
          content: undefined,
          model: 'gpt-4',
          toolCalls,
          createdAt: '2024-01-15T10:00:00Z',
        },
      ]

      const result = convertGramMessagesToExported(messages)
      const content = result.messages[0].message.content

      expect(content).toHaveLength(1)
      expect(content[0]).toMatchObject({
        type: 'tool-call',
        args: { key: 'value' },
      })
    })

    it('defaults to empty values for missing tool call fields', () => {
      const toolCalls = JSON.stringify([
        {
          // Missing id, function.name, arguments
        },
      ])

      const messages: GramChatMessage[] = [
        {
          id: 'msg-1',
          role: 'assistant',
          content: undefined,
          model: 'gpt-4',
          toolCalls,
          createdAt: '2024-01-15T10:00:00Z',
        },
      ]

      const result = convertGramMessagesToExported(messages)
      const content = result.messages[0].message.content

      expect(content).toHaveLength(1)
      expect(content[0]).toMatchObject({
        type: 'tool-call',
        toolCallId: '',
        toolName: '',
      })
    })
  })

  describe('date parsing', () => {
    it('parses ISO string dates', () => {
      const messages: GramChatMessage[] = [
        {
          id: 'msg-1',
          role: 'user',
          content: 'Test',
          model: 'gpt-4',
          createdAt: '2024-06-15T14:30:00.000Z',
        },
      ]

      const result = convertGramMessagesToExported(messages)
      const createdAt = result.messages[0].message.createdAt

      expect(createdAt).toBeInstanceOf(Date)
      expect(createdAt.toISOString()).toBe('2024-06-15T14:30:00.000Z')
    })

    it('handles Date objects directly', () => {
      const date = new Date('2024-06-15T14:30:00.000Z')
      const messages: GramChatMessage[] = [
        {
          id: 'msg-1',
          role: 'user',
          content: 'Test',
          model: 'gpt-4',
          createdAt: date,
        },
      ]

      const result = convertGramMessagesToExported(messages)
      const createdAt = result.messages[0].message.createdAt

      expect(createdAt).toBeInstanceOf(Date)
      expect(createdAt.getTime()).toBe(date.getTime())
    })
  })

  describe('role normalization', () => {
    it('normalizes unknown roles to assistant', () => {
      const messages: GramChatMessage[] = [
        {
          id: 'msg-1',
          role: 'tool', // Unknown role
          content: 'Tool result',
          model: 'gpt-4',
          createdAt: '2024-01-15T10:00:00Z',
        },
      ]

      const result = convertGramMessagesToExported(messages)

      expect(result.messages[0].message.role).toBe('assistant')
    })

    it('preserves user role', () => {
      const messages: GramChatMessage[] = [
        {
          id: 'msg-1',
          role: 'user',
          content: 'Hello',
          model: 'gpt-4',
          createdAt: '2024-01-15T10:00:00Z',
        },
      ]

      const result = convertGramMessagesToExported(messages)

      expect(result.messages[0].message.role).toBe('user')
    })

    it('preserves assistant role', () => {
      const messages: GramChatMessage[] = [
        {
          id: 'msg-1',
          role: 'assistant',
          content: 'Hi!',
          model: 'gpt-4',
          createdAt: '2024-01-15T10:00:00Z',
        },
      ]

      const result = convertGramMessagesToExported(messages)

      expect(result.messages[0].message.role).toBe('assistant')
    })
  })

  describe('message metadata', () => {
    it('includes metadata on user messages', () => {
      const messages: GramChatMessage[] = [
        {
          id: 'msg-1',
          role: 'user',
          content: 'Test',
          model: 'gpt-4',
          createdAt: '2024-01-15T10:00:00Z',
        },
      ]

      const result = convertGramMessagesToExported(messages)
      const msg = result.messages[0].message

      expect(msg.metadata).toBeDefined()
      expect(msg.metadata.custom).toEqual({})
    })

    it('includes attachments array on user messages', () => {
      const messages: GramChatMessage[] = [
        {
          id: 'msg-1',
          role: 'user',
          content: 'Test',
          model: 'gpt-4',
          createdAt: '2024-01-15T10:00:00Z',
        },
      ]

      const result = convertGramMessagesToExported(messages)
      const msg = result.messages[0].message

      if (msg.role === 'user') {
        expect(msg.attachments).toEqual([])
      }
    })

    it('includes metadata on assistant messages', () => {
      const messages: GramChatMessage[] = [
        {
          id: 'msg-1',
          role: 'assistant',
          content: 'Test',
          model: 'gpt-4',
          createdAt: '2024-01-15T10:00:00Z',
        },
      ]

      const result = convertGramMessagesToExported(messages)
      const msg = result.messages[0].message

      expect(msg.metadata).toBeDefined()
      if (msg.role === 'assistant') {
        expect(msg.metadata.unstable_annotations).toEqual([])
        expect(msg.metadata.unstable_data).toEqual([])
        expect(msg.metadata.steps).toEqual([])
      }
    })
  })
})
