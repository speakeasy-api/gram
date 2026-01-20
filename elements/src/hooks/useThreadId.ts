import { useAssistantState } from '@assistant-ui/react'

/**
 * Hook to access the current thread ID from the Elements chat.
 * Returns the thread ID (remoteId) when a thread is active, or null if no thread is loaded.
 *
 * @example
 * ```tsx
 * import { useThreadId } from '@gram-ai/elements'
 *
 * function ShareButton() {
 *   const { threadId } = useThreadId()
 *
 *   const handleShare = () => {
 *     if (!threadId) return
 *     const shareUrl = `${window.location.href}?threadId=${threadId}`
 *     navigator.clipboard.writeText(shareUrl)
 *   }
 *
 *   return <button onClick={handleShare} disabled={!threadId}>Share</button>
 * }
 * ```
 */
export function useThreadId(): { threadId: string | null } {
  const threadId = useAssistantState(
    ({ threadListItem }) => threadListItem.remoteId ?? null
  )

  return { threadId }
}
