/**
 * Hook to record the current chat conversation as a cassette file.
 *
 * Must be used inside a `GramElementsProvider` (or any `AssistantRuntimeProvider`).
 *
 * @example
 * ```tsx
 * function RecordableChat() {
 *   const { isRecording, startRecording, stopRecording, download } = useRecordCassette()
 *   return (
 *     <>
 *       <Chat />
 *       <button onClick={startRecording}>Record</button>
 *       <button onClick={stopRecording}>Stop</button>
 *       <button onClick={() => download('demo')}>Save Cassette</button>
 *     </>
 *   )
 * }
 * ```
 */

import { useThreadRuntime } from '@assistant-ui/react'
import { useCallback, useRef, useState, useSyncExternalStore } from 'react'
import { recordCassette } from '@/lib/cassette'

export function useRecordCassette(): {
  /** Whether recording is currently active. */
  isRecording: boolean
  /** Current number of messages in the thread. */
  messageCount: number
  /** Start recording from the current point in the conversation. */
  startRecording: () => void
  /** Stop recording. */
  stopRecording: () => void
  /** Downloads the recorded conversation as a `.cassette.json` file. */
  download: (filename?: string) => void
} {
  const runtime = useThreadRuntime()
  const [isRecording, setIsRecording] = useState(false)
  const startIndexRef = useRef(0)

  // Subscribe to runtime state to get reactive message count
  const messageCount = useSyncExternalStore(
    (cb) => runtime.subscribe(cb),
    () => runtime.getState().messages.length
  )

  const startRecording = useCallback(() => {
    startIndexRef.current = runtime.getState().messages.length
    setIsRecording(true)
  }, [runtime])

  const stopRecording = useCallback(() => {
    setIsRecording(false)
  }, [])

  const download = useCallback(
    (filename?: string) => {
      const state = runtime.getState()
      const messages = state.messages.slice(startIndexRef.current)
      const cassette = recordCassette(messages)

      const json = JSON.stringify(cassette, null, 2)
      const blob = new Blob([json], { type: 'application/json' })
      const url = URL.createObjectURL(blob)

      const a = document.createElement('a')
      a.href = url
      a.download = `${filename ?? 'cassette'}.cassette.json`
      document.body.appendChild(a)
      a.click()
      document.body.removeChild(a)
      URL.revokeObjectURL(url)
    },
    [runtime]
  )

  return { isRecording, messageCount, startRecording, stopRecording, download }
}
