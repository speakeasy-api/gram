/* eslint-disable @typescript-eslint/no-explicit-any */

/**
 * Message format converter for Gram API <-> assistant-ui.
 *
 * The Gram API returns chat messages in its own schema (GramChatMessage),
 * while assistant-ui expects messages in its internal ThreadMessage format.
 * This module bridges that gap by converting between the two formats.
 *
 * Main export: `convertGramMessagesToExported` - converts an array of Gram
 * messages into an ExportedMessageRepository with parent-child relationships
 * for conversation threading.
 */

import type {
  ExportedMessageRepository,
  ThreadMessage,
  ThreadUserMessagePart,
  ThreadAssistantMessagePart,
  TextMessagePart,
} from '@assistant-ui/react'
import type {
  Message,
  UserMessage,
  AssistantMessage,
  ToolResponseMessage,
} from '@openrouter/sdk/models'
import { UIMessage } from 'ai'

/**
 * Represents a chat message from the Gram API.
 * This mirrors the ChatMessage type from @gram/sdk without requiring the SDK dependency.
 */
export type GramChatMessage = Message & {
  id: string
  model: string
  created_at: Date | string
}

/**
 * Represents a chat from the Gram API.
 */
export interface GramChat {
  id: string
  title: string
  userId: string
  numMessages: number
  messages: GramChatMessage[]
  createdAt: Date | string
  updatedAt: Date | string
}

/**
 * Represents a chat overview from the Gram API (without full messages).
 */
export interface GramChatOverview {
  id: string
  title: string
  userId: string
  numMessages: number
  createdAt: Date | string
  updatedAt: Date | string
}

/**
 * Parses a date that might be a string or Date object.
 */
function parseDate(date: Date | string): Date {
  return typeof date === 'string' ? new Date(date) : date
}

/**
 * Builds content parts for a user message.
 */
function buildUserContentParts(msg: GramChatMessage): ThreadUserMessagePart[] {
  if (msg.role !== 'user') {
    return []
  }

  if (typeof msg.content === 'string' || !msg.content) {
    return [
      {
        type: 'text',
        text: msg.content ?? '',
      },
    ]
  }

  const parts: ThreadUserMessagePart[] = []

  for (const item of msg.content) {
    switch (item.type) {
      case 'text':
        parts.push({
          type: 'text',
          text: item.text,
        })
        break
      case 'image_url':
        parts.push({
          type: 'image',
          image: (item as any).image_url?.url as FIXME<
            string,
            'Fixed by switching to Gram TS SDK.'
          >,
        })
        break
      case 'input_audio': {
        const format = (item as any).input_audio?.format as FIXME<
          string,
          'Fixed by switching to Gram TS SDK.'
        >
        if (format === 'mp3' || format === 'wav') {
          parts.push({
            type: 'audio',
            audio: {
              data: (item as any).input_audio.data as FIXME<
                string,
                'Fixed by switching to Gram TS SDK.'
              >,
              format: format,
            },
          })
        }
        break
      }
      default:
        parts.push({
          type: 'text',
          text: '',
        })
        break
    }
  }

  return parts
}

/**
 * Builds content parts for an assistant message, including tool calls.
 */
function buildAssistantContentParts(
  msg: GramChatMessage
): ThreadAssistantMessagePart[] {
  if (msg.role !== 'assistant') {
    return []
  }

  if (typeof msg.content === 'string' || !msg.content) {
    return [
      {
        type: 'text',
        text: msg.content ?? '',
      },
    ]
  }

  const parts: ThreadAssistantMessagePart[] = []

  const toolCallsJSON = (msg as any).tool_calls as FIXME<
    string | undefined,
    'Fixed by switching to Gram TS SDK.'
  >

  let toolCalls = tryParseJSON(toolCallsJSON || '[]')
  if (!Array.isArray(toolCalls)) {
    console.warn('Invalid tool_calls format, expected an array.')
    toolCalls = []
  }

  for (const tc of toolCalls) {
    const args = tc.function?.arguments ?? tc.args ?? {}
    const argsText = typeof args === 'string' ? args : JSON.stringify(args)
    parts.push({
      type: 'tool-call',
      toolCallId: tc.id ?? tc.toolCallId ?? '',
      toolName: tc.function?.name ?? tc.toolName ?? '',
      args: typeof args === 'string' ? JSON.parse(args) : args,
      argsText,
      result: undefined,
    } as ThreadAssistantMessagePart)
  }

  // Return at least an empty text part if no content
  if (parts.length === 0) {
    parts.push({
      type: 'text',
      text: '',
    } as TextMessagePart)
  }

  return parts
}

function buildSystemContentParts(msg: GramChatMessage): [TextMessagePart] {
  if (msg.role !== 'system') {
    return [{ type: 'text', text: '' }]
  }

  if (typeof msg.content === 'string' || !msg.content) {
    return [{ type: 'text', text: msg.content ?? '' }]
  }

  const text: string[] = []

  for (const item of msg.content) {
    if (item.type !== 'text') {
      continue
    }
    text.push(item.text)
  }

  return [{ type: 'text', text: text.join('\n') }]
}

/**
 * Converts a single Gram ChatMessage to a ThreadMessage.
 */
function convertGramMessageToThreadMessage(
  msg: GramChatMessage
): ThreadMessage {
  const createdAt = parseDate(msg.created_at)

  const baseMetadata = {
    unstable_state: undefined,
    unstable_annotations: undefined,
    unstable_data: undefined,
    steps: undefined,
    submittedFeedback: undefined,
    custom: {},
  }

  if (msg.role === 'user') {
    return {
      id: msg.id,
      role: 'user',
      createdAt,
      content: buildUserContentParts(msg),
      attachments: [],
      metadata: baseMetadata,
    }
  }

  if (msg.role === 'system') {
    return {
      id: msg.id,
      role: 'system',
      createdAt,
      content: buildSystemContentParts(msg),
      metadata: baseMetadata,
    }
  }

  // Assistant message
  return {
    id: msg.id,
    role: 'assistant',
    createdAt,
    content: buildAssistantContentParts(msg),
    status: { type: 'complete', reason: 'stop' },
    metadata: {
      unstable_state: null,
      unstable_annotations: [],
      unstable_data: [],
      steps: [],
      submittedFeedback: undefined,
      custom: {},
    },
  }
}

/**
 * Converts an array of Gram ChatMessages to an ExportedMessageRepository.
 * Creates parent-child relationships based on message order.
 *
 * Note: System messages are filtered out because assistant-ui's
 * `fromThreadMessageLike` doesn't support them in the exported format.
 */
export function convertGramMessagesToExported(
  messages: GramChatMessage[]
): ExportedMessageRepository {
  if (messages.length === 0) {
    return { messages: [], headId: null }
  }

  const exportedMessages: ExportedMessageRepository['messages'] = []
  let prevId: string | null = null

  for (const msg of messages) {
    // Skip system messages - they're not supported in the exported message format
    if (msg.role === 'system') {
      continue
    }

    const threadMessage = convertGramMessageToThreadMessage(msg)
    exportedMessages.push({
      message: threadMessage,
      parentId: prevId,
      runConfig: undefined,
    })
    prevId = msg.id
  }

  return {
    messages: exportedMessages,
    headId: prevId,
  }
}

export function convertGramMessagesToUIMessages(messages: GramChatMessage[]): {
  headId: string | null
  messages: { parentId: string | null; message: UIMessage }[]
} {
  if (messages.length === 0) {
    return { messages: [], headId: null }
  }

  const toolCallResults = new Map<string, ToolResponseMessage>()
  for (const msg of messages) {
    if (msg.role !== 'tool') {
      continue
    }
    const id = (msg as any).tool_call_id
    if (typeof id !== 'string') {
      continue
    }

    toolCallResults.set(id, msg as ToolResponseMessage)
  }

  const uiMessages: { parentId: string | null; message: UIMessage }[] = []
  let prevId: string | null = null

  // Track tool call IDs across messages to deduplicate. The server accumulates
  // all tool calls from a turn into each message, so without this, every
  // assistant message in a multi-step tool use flow would show the full count.
  const seenToolCallIds = new Set<string>()

  for (const msg of messages) {
    switch (msg.role) {
      case 'developer':
      case 'tool':
        continue
      case 'system': {
        uiMessages.push({
          parentId: prevId,
          message: {
            id: msg.id,
            role: 'system',
            parts: [
              {
                type: 'text',
                text:
                  typeof msg.content === 'string'
                    ? msg.content
                    : Array.isArray(msg.content)
                      ? msg.content
                          .filter((item) => item.type === 'text')
                          .map((item) => item.text)
                          .join('\n')
                      : '',
              },
            ],
          },
        })
        break
      }
      case 'user': {
        seenToolCallIds.clear()
        uiMessages.push({
          parentId: prevId,
          message: {
            id: msg.id,
            role: 'user',
            parts: convertGramMessagePartsToUIMessageParts(
              msg,
              toolCallResults
            ),
          },
        })
        break
      }
      case 'assistant': {
        const uiMessage = {
          parentId: prevId,
          message: {
            id: msg.id,
            role: 'assistant',
            parts: convertGramMessagePartsToUIMessageParts(
              msg,
              toolCallResults,
              seenToolCallIds
            ),
          } satisfies UIMessage,
        }
        uiMessages.push(uiMessage)

        break
      }
    }

    prevId = msg.id
  }

  return {
    messages: uiMessages,
    headId: prevId,
  }
}

export function convertGramMessagePartsToUIMessageParts(
  msg: UserMessage | AssistantMessage,
  toolResults: Map<string, ToolResponseMessage>,
  seenToolCallIds?: Set<string>
): UIMessage['parts'] {
  const uiparts: UIMessage['parts'] = []

  if (typeof msg.content === 'string' && msg.content) {
    uiparts.push({
      type: 'text',
      text: msg.content,
    })
  }

  const content = Array.isArray(msg.content) ? msg.content : []
  for (const p of content) {
    switch (p.type) {
      case 'text': {
        uiparts.push({
          type: 'text',
          text: p.text,
        })
        break
      }
      case 'image_url': {
        const url = (p as any).image_url?.url as FIXME<
          string | undefined,
          'Fixed by switching to Gram TS SDK.'
        >
        if (!url) {
          break
        }

        uiparts.push({
          type: 'file',
          url,
          mediaType: mediaTypeFromURL(url),
        })
        break
      }
      case 'input_audio': {
        const url = (p as any).input_audio?.data as FIXME<
          string | undefined,
          'Fixed by switching to Gram TS SDK.'
        >
        if (!url) {
          break
        }

        uiparts.push({
          type: 'file',
          url,
          mediaType: mediaTypeFromURL(url),
        })
        break
      }
    }
  }

  if (msg.role === 'assistant' && msg.reasoning) {
    uiparts.push({
      type: 'reasoning',
      text: msg.reasoning,
    })
  }

  if (msg.role === 'assistant' && (msg as any).tool_calls) {
    const toolCallsJSON = (msg as any).tool_calls as FIXME<
      string,
      'Fixed by switching to Gram TS SDK.'
    >
    let toolCalls = tryParseJSON<AssistantMessage['toolCalls']>(
      toolCallsJSON || '[]'
    )
    if (!Array.isArray(toolCalls)) {
      console.warn('Invalid tool_calls format, expected an array.')
      toolCalls = []
    }

    for (const tc of toolCalls) {
      // The server accumulates all tool calls from a turn into each message's
      // tool_calls field. Deduplicate across messages so each tool call only
      // appears in the first message that references it.
      if (seenToolCallIds?.has(tc.id)) continue
      seenToolCallIds?.add(tc.id)

      const content = toolResults.get(tc.id)?.content
      uiparts.push({
        type: 'dynamic-tool',
        toolCallId: tc.id,
        toolName: tc.function?.name ?? '',
        state: 'output-available',
        input: tc.function?.arguments ?? {},
        output: typeof content === 'string' ? tryParseJSON(content) : '',
      })
    }
  }

  return uiparts
}

function mediaTypeFromURL(url: string): string {
  const unspecified = 'unknown/unknown'
  if (!url.startsWith('data:')) {
    return unspecified
  }

  const match = url.match(/^data:([^;]+);/)
  return match?.[1] || unspecified
}

function tryParseJSON<T = any>(str: string): T | null {
  try {
    return JSON.parse(str) as T
  } catch {
    return null
  }
}
