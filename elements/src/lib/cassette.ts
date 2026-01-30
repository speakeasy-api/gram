/**
 * Cassette format for recording and replaying chat conversations.
 *
 * A cassette captures a conversation as a JSON-serializable array of messages,
 * mirroring assistant-ui's ThreadMessage content structure (minus IDs/metadata).
 *
 * Intended workflow:
 * 1. Use `useRecordCassette()` to capture a live conversation
 * 2. Save the cassette as a JSON file
 * 3. Pass it to `<Replay cassette={...}>` to play it back
 */

import { createUIMessageStream, type ChatTransport, type UIMessage } from 'ai'
import type { ThreadMessage } from '@assistant-ui/react'

// ---------------------------------------------------------------------------
// Cassette types
// ---------------------------------------------------------------------------

export type CassetteTextPart = { type: 'text'; text: string }
export type CassetteReasoningPart = { type: 'reasoning'; text: string }
export type CassetteToolCallPart = {
  type: 'tool-call'
  toolCallId: string
  toolName: string
  args: unknown
  result?: unknown
}

export type CassettePart =
  | CassetteTextPart
  | CassetteReasoningPart
  | CassetteToolCallPart

export interface CassetteMessage {
  role: 'user' | 'assistant'
  content: CassettePart[]
}

export interface Cassette {
  messages: CassetteMessage[]
}

// ---------------------------------------------------------------------------
// Replay options
// ---------------------------------------------------------------------------

export interface ReplayOptions {
  /** Milliseconds per character when streaming text. @default 15 */
  typingSpeed?: number
  /** Milliseconds to wait before showing each user message. @default 800 */
  userMessageDelay?: number
  /** Milliseconds to wait before the assistant starts "typing". @default 400 */
  assistantStartDelay?: number
  /** Called when the full replay sequence finishes. */
  onComplete?: () => void
}

// ---------------------------------------------------------------------------
// Recording: ThreadMessage[] → Cassette
// ---------------------------------------------------------------------------

/**
 * Converts assistant-ui ThreadMessages into a serializable Cassette.
 * System messages are filtered out since they aren't displayed.
 */
export function recordCassette(messages: readonly ThreadMessage[]): Cassette {
  const cassetteMessages: CassetteMessage[] = []

  for (const msg of messages) {
    if (msg.role === 'system') continue

    const parts: CassettePart[] = []

    for (const part of msg.content) {
      switch (part.type) {
        case 'text':
          if (part.text) {
            parts.push({ type: 'text', text: part.text })
          }
          break
        case 'reasoning':
          if (part.text) {
            parts.push({ type: 'reasoning', text: part.text })
          }
          break
        case 'tool-call':
          parts.push({
            type: 'tool-call',
            toolCallId: part.toolCallId,
            toolName: part.toolName,
            args: part.args,
            result: part.result,
          })
          break
        // Skip image, file, audio, source, data parts for now
      }
    }

    if (parts.length > 0) {
      cassetteMessages.push({
        role: msg.role as 'user' | 'assistant',
        content: parts,
      })
    }
  }

  return { messages: cassetteMessages }
}

// ---------------------------------------------------------------------------
// Playback: Cassette → ChatTransport
// ---------------------------------------------------------------------------

/** Sleep that respects AbortSignal for clean cancellation. */
function sleep(ms: number, signal?: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    if (signal?.aborted) {
      reject(new DOMException('Aborted', 'AbortError'))
      return
    }
    const timeout = setTimeout(resolve, ms)
    signal?.addEventListener(
      'abort',
      () => {
        clearTimeout(timeout)
        reject(new DOMException('Aborted', 'AbortError'))
      },
      { once: true }
    )
  })
}

/**
 * Creates a ChatTransport that replays pre-recorded assistant messages
 * from a cassette. Each call to `sendMessages` (triggered by a user message
 * being appended) returns the next assistant response as a stream.
 */
export function createReplayTransport(
  cassette: Cassette,
  options?: ReplayOptions
): ChatTransport<UIMessage> {
  const typingSpeed = options?.typingSpeed ?? 15
  const assistantStartDelay = options?.assistantStartDelay ?? 400

  // Cursor tracking which message index to serve next.
  // Starts at 0 and advances past user+assistant pairs.
  let cursor = 0

  return {
    sendMessages: async ({ abortSignal }) => {
      // The user message was already appended by ReplayController.
      // Advance cursor past it (it should be pointing at a user message).
      if (
        cursor < cassette.messages.length &&
        cassette.messages[cursor].role === 'user'
      ) {
        cursor++
      }

      // Collect the next assistant message(s).
      // Multiple consecutive assistant messages are possible (e.g. multi-step tool calls).
      const assistantMessages: CassetteMessage[] = []
      while (
        cursor < cassette.messages.length &&
        cassette.messages[cursor].role === 'assistant'
      ) {
        assistantMessages.push(cassette.messages[cursor])
        cursor++
      }

      // Return a stream that emits the pre-recorded content
      return createUIMessageStream({
        execute: async ({ writer }) => {
          if (assistantMessages.length === 0) {
            return
          }

          try {
            await sleep(assistantStartDelay, abortSignal)
          } catch {
            return // aborted
          }

          for (const message of assistantMessages) {
            for (const part of message.content) {
              try {
                await writeReplayPart(writer, part, typingSpeed, abortSignal)
              } catch {
                return // aborted
              }
            }
          }
        },
      })
    },
    reconnectToStream: async () => {
      return null
    },
  }
}

// ---------------------------------------------------------------------------
// Stream writing helpers
// ---------------------------------------------------------------------------

interface StreamWriter {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  write(part: any): void
}

async function writeReplayPart(
  writer: StreamWriter,
  part: CassettePart,
  typingSpeed: number,
  abortSignal?: AbortSignal
): Promise<void> {
  switch (part.type) {
    case 'text': {
      const partId = crypto.randomUUID()
      writer.write({ type: 'text-start', id: partId })
      for (const char of part.text) {
        writer.write({ type: 'text-delta', id: partId, delta: char })
        await sleep(typingSpeed, abortSignal)
      }
      writer.write({ type: 'text-end', id: partId })
      break
    }

    case 'reasoning': {
      const partId = crypto.randomUUID()
      writer.write({ type: 'reasoning-start', id: partId })
      for (const char of part.text) {
        writer.write({ type: 'reasoning-delta', id: partId, delta: char })
        await sleep(typingSpeed, abortSignal)
      }
      writer.write({ type: 'reasoning-end', id: partId })
      break
    }

    case 'tool-call': {
      writer.write({
        type: 'tool-input-available',
        toolCallId: part.toolCallId,
        toolName: part.toolName,
        input: part.args,
      })
      if (part.result !== undefined) {
        // Brief pause to simulate tool execution
        await sleep(300, abortSignal)
        writer.write({
          type: 'tool-output-available',
          toolCallId: part.toolCallId,
          output: part.result,
        })
      }
      break
    }
  }
}
