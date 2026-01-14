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

/**
 * Represents a chat message from the Gram API.
 * This mirrors the ChatMessage type from @gram/sdk without requiring the SDK dependency.
 */
export interface GramChatMessage {
  id: string
  role: string
  content?: string
  model: string
  toolCallId?: string
  toolCalls?: string
  createdAt: Date | string
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
 * Normalizes a role string to valid ThreadMessage roles.
 */
function normalizeRole(role: string): 'user' | 'assistant' | 'system' {
  if (role === 'user') return 'user'
  if (role === 'assistant') return 'assistant'
  if (role === 'system') return 'system'
  // Tool role messages should be handled differently, but for now treat as assistant
  return 'assistant'
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
  const parts: ThreadUserMessagePart[] = []

  if (msg.content) {
    parts.push({
      type: 'text',
      text: msg.content,
    } as TextMessagePart)
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

/**
 * Builds content parts for an assistant message, including tool calls.
 */
function buildAssistantContentParts(
  msg: GramChatMessage
): ThreadAssistantMessagePart[] {
  const parts: ThreadAssistantMessagePart[] = []

  if (msg.content) {
    parts.push({
      type: 'text',
      text: msg.content,
    } as TextMessagePart)
  }

  if (msg.toolCalls) {
    try {
      const toolCalls = JSON.parse(msg.toolCalls)
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
    } catch {
      // Ignore JSON parse errors for tool calls
    }
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

/**
 * Converts a single Gram ChatMessage to a ThreadMessage.
 */
function convertGramMessageToThreadMessage(
  msg: GramChatMessage
): ThreadMessage {
  const role = normalizeRole(msg.role)
  const createdAt = parseDate(msg.createdAt)

  const baseMetadata = {
    unstable_state: undefined,
    unstable_annotations: undefined,
    unstable_data: undefined,
    steps: undefined,
    submittedFeedback: undefined,
    custom: {},
  }

  if (role === 'user') {
    return {
      id: msg.id,
      role: 'user',
      createdAt,
      content: buildUserContentParts(msg),
      attachments: [],
      metadata: baseMetadata,
    }
  }

  if (role === 'system') {
    return {
      id: msg.id,
      role: 'system',
      createdAt,
      content: [{ type: 'text', text: msg.content ?? '' }],
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
