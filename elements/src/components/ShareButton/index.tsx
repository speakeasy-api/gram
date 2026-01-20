'use client'

import { Link } from 'lucide-react'
import { useCallback } from 'react'

import { Button } from '@/components/ui/button'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { useThreadId } from '@/hooks/useThreadId'
import { cn } from '@/lib/utils'

export interface ShareButtonProps {
  /**
   * Called when the share action completes.
   * Receives the share URL on success, or an Error on failure.
   * Use this to show toast notifications or track analytics.
   */
  onShare?: (result: { url: string } | { error: Error }) => void

  /**
   * Custom URL builder. By default, appends `?threadId={id}` to current URL.
   * Return the full share URL.
   */
  buildShareUrl?: (threadId: string) => string

  /**
   * Button variant
   * @default "ghost"
   */
  variant?: 'ghost' | 'outline' | 'default'

  /**
   * Button size
   * @default "sm"
   */
  size?: 'sm' | 'default' | 'lg' | 'icon'

  /**
   * Additional CSS classes
   */
  className?: string

  /**
   * Custom button content. If not provided, shows icon + "Share chat"
   */
  children?: React.ReactNode
}

/**
 * A button component for sharing the current chat thread.
 * Copies a shareable URL to the clipboard when clicked.
 *
 * @example
 * ```tsx
 * import { ShareButton } from '@gram-ai/elements'
 * import { toast } from 'sonner'
 *
 * function MyChat() {
 *   return (
 *     <ShareButton
 *       onShare={(result) => {
 *         if ('url' in result) {
 *           toast.success('Chat link copied!')
 *         } else {
 *           toast.error(result.error.message)
 *         }
 *       }}
 *     />
 *   )
 * }
 * ```
 */
export function ShareButton({
  onShare,
  buildShareUrl,
  variant = 'ghost',
  size = 'sm',
  className,
  children,
}: ShareButtonProps) {
  const { threadId } = useThreadId()

  const handleShare = useCallback(async () => {
    if (!threadId) {
      onShare?.({
        error: new Error('No chat to share yet. Send a message first.'),
      })
      return
    }

    try {
      // Build share URL
      const shareUrl = buildShareUrl
        ? buildShareUrl(threadId)
        : (() => {
            const url = new URL(window.location.href)
            url.searchParams.set('threadId', threadId)
            return url.toString()
          })()

      // Copy to clipboard
      await navigator.clipboard.writeText(shareUrl)
      onShare?.({ url: shareUrl })
    } catch (error) {
      onShare?.({
        error:
          error instanceof Error ? error : new Error('Failed to copy link'),
      })
    }
  }, [threadId, buildShareUrl, onShare])

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          variant={variant}
          size={size}
          onClick={handleShare}
          disabled={!threadId}
          className={cn('aui-share-button', className)}
          aria-label="Share chat"
        >
          {children ?? (
            <>
              <Link className="mr-2 size-4" />
              Share chat
            </>
          )}
        </Button>
      </TooltipTrigger>
      <TooltipContent>
        {threadId
          ? 'Copy chat link to clipboard'
          : 'Send a message first to share'}
      </TooltipContent>
    </Tooltip>
  )
}
