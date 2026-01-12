import { type FC, useState, useRef, useEffect } from 'react'
import { PlusIcon, MessageSquareIcon, PencilIcon, CheckIcon, XIcon } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { useChatList, type ChatOverview } from '@/hooks/useChatList'
import { useElements } from '@/hooks/useElements'
import { cn } from '@/lib/utils'
import { useSession } from '@/hooks/useSession'
import { useMutation, useQueryClient } from '@tanstack/react-query'

const GRAM_API_URL = 'https://localhost:8080'

interface ChatHistoryProps {
  /**
   * Currently selected chat ID
   */
  selectedChatId?: string | null
  /**
   * Callback when a chat is selected
   */
  onSelectChat?: (chatId: string) => void
  /**
   * Callback when "New Chat" is clicked
   */
  onNewChat?: () => void
  /**
   * Custom getSession function (uses default from ElementsProvider if not provided)
   */
  getSession?: (init: { projectSlug: string }) => Promise<string>
}

/**
 * ChatHistory displays a list of previous chat conversations.
 * This is a simpler alternative to ThreadList that directly uses the Gram API.
 */
export const ChatHistory: FC<ChatHistoryProps> = ({
  selectedChatId,
  onSelectChat,
  onNewChat,
  getSession: customGetSession,
}) => {
  const { config, chatId, startNewChat, loadChat } = useElements()
  const queryClient = useQueryClient()

  // Use chatId from context if selectedChatId is not provided
  const currentSelectedId = selectedChatId ?? chatId

  // Default onNewChat handler uses startNewChat from context
  const handleNewChat = onNewChat ?? startNewChat

  // Default onSelectChat handler uses loadChat from context
  const handleSelectChat = onSelectChat ?? loadChat

  // Use a default getSession if not provided
  const getSession =
    customGetSession ??
    (async ({ projectSlug }: { projectSlug: string }) => {
      const response = await fetch('/chat/session', {
        method: 'POST',
        headers: { 'Gram-Project': projectSlug },
      })
      const data = await response.json()
      return data.client_token
    })

  const session = useSession({ getSession, projectSlug: config.projectSlug })

  const { data: chats, isLoading, error } = useChatList({
    getSession,
    projectSlug: config.projectSlug,
  })

  // Mutation for renaming chats
  const renameMutation = useMutation({
    mutationFn: async ({ chatId: id, title }: { chatId: string; title: string }) => {
      if (!session) {
        throw new Error('No session found')
      }
      const response = await fetch(`${GRAM_API_URL}/rpc/chat.rename`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Gram-Project': config.projectSlug,
          'Gram-Chat-Session': session,
        },
        body: JSON.stringify({ id, title }),
      })
      if (!response.ok) {
        throw new Error(`Failed to rename chat: ${response.statusText}`)
      }
      return response.json()
    },
    onSuccess: () => {
      // Invalidate the chat list to refetch with updated titles
      queryClient.invalidateQueries({ queryKey: ['chatList', config.projectSlug] })
    },
  })

  const handleRename = (chatIdToRename: string, newTitle: string) => {
    renameMutation.mutate({ chatId: chatIdToRename, title: newTitle })
  }

  return (
    <div className="flex flex-col gap-1.5">
      <NewChatButton onClick={handleNewChat} />

      {isLoading && <ChatHistorySkeleton />}

      {error && (
        <div className="text-destructive px-3 py-2 text-sm">
          Failed to load chats
        </div>
      )}

      {!isLoading && !error && chats && (
        <ChatHistoryItems
          chats={chats}
          selectedChatId={currentSelectedId}
          onSelectChat={handleSelectChat}
          onRename={handleRename}
        />
      )}
    </div>
  )
}

const NewChatButton: FC<{ onClick?: () => void }> = ({ onClick }) => {
  return (
    <Button
      onClick={onClick}
      className="hover:bg-muted data-active:bg-muted flex items-center justify-start gap-1 rounded-lg px-2.5 py-2 text-start"
      variant="ghost"
    >
      <PlusIcon className="size-4" />
      New Chat
    </Button>
  )
}

const ChatHistorySkeleton: FC = () => {
  return (
    <>
      {Array.from({ length: 5 }, (_, i) => (
        <div
          key={i}
          role="status"
          aria-label="Loading chats"
          aria-live="polite"
          className="flex items-center gap-2 rounded-md px-3 py-2"
        >
          <Skeleton className="h-[22px] grow" />
        </div>
      ))}
    </>
  )
}

const ChatHistoryItems: FC<{
  chats: ChatOverview[]
  selectedChatId?: string | null
  onSelectChat?: (chatId: string) => void
  onRename?: (chatId: string, newTitle: string) => void
}> = ({ chats, selectedChatId, onSelectChat, onRename }) => {
  if (chats.length === 0) {
    return (
      <div className="text-muted-foreground px-3 py-2 text-sm">
        No previous chats
      </div>
    )
  }

  return (
    <>
      {chats.map((chat) => (
        <ChatHistoryItem
          key={chat.id}
          chat={chat}
          isSelected={chat.id === selectedChatId}
          onSelect={() => onSelectChat?.(chat.id)}
          onRename={onRename}
        />
      ))}
    </>
  )
}

const ChatHistoryItem: FC<{
  chat: ChatOverview
  isSelected?: boolean
  onSelect?: () => void
  onRename?: (chatId: string, newTitle: string) => void
}> = ({ chat, isSelected, onSelect, onRename }) => {
  const [isEditing, setIsEditing] = useState(false)
  const [editValue, setEditValue] = useState(chat.title || '')
  const inputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    if (isEditing && inputRef.current) {
      inputRef.current.focus()
      inputRef.current.select()
    }
  }, [isEditing])

  const handleSave = () => {
    const trimmed = editValue.trim()
    if (trimmed && trimmed !== chat.title) {
      onRename?.(chat.id, trimmed)
    }
    setIsEditing(false)
  }

  const handleCancel = () => {
    setEditValue(chat.title || '')
    setIsEditing(false)
  }

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter') {
      handleSave()
    } else if (e.key === 'Escape') {
      handleCancel()
    }
  }

  if (isEditing) {
    return (
      <div className="flex w-full items-center gap-1 rounded-lg px-2 py-1.5">
        <MessageSquareIcon className="text-muted-foreground size-4 shrink-0" />
        <input
          ref={inputRef}
          type="text"
          value={editValue}
          onChange={(e) => setEditValue(e.target.value)}
          onKeyDown={handleKeyDown}
          onBlur={handleSave}
          className="bg-background border-input flex-1 rounded border px-2 py-1 text-sm outline-none"
        />
        <button
          onClick={handleSave}
          className="text-muted-foreground hover:text-foreground p-1"
          aria-label="Save"
        >
          <CheckIcon className="size-3.5" />
        </button>
        <button
          onClick={handleCancel}
          className="text-muted-foreground hover:text-foreground p-1"
          aria-label="Cancel"
        >
          <XIcon className="size-3.5" />
        </button>
      </div>
    )
  }

  return (
    <button
      onClick={onSelect}
      className={cn(
        'group flex w-full cursor-pointer items-center gap-2 rounded-lg px-3 py-2 text-start transition-all',
        'hover:bg-muted focus-visible:bg-muted focus-visible:ring-ring focus-visible:ring-2 focus-visible:outline-none',
        isSelected && 'bg-muted'
      )}
    >
      <MessageSquareIcon className="text-muted-foreground size-4 shrink-0" />
      <span className="flex-1 truncate text-sm">{chat.title || 'New Chat'}</span>
      {onRename && (
        <span
          role="button"
          tabIndex={0}
          onClick={(e) => {
            e.stopPropagation()
            setIsEditing(true)
          }}
          onKeyDown={(e) => {
            if (e.key === 'Enter' || e.key === ' ') {
              e.stopPropagation()
              setIsEditing(true)
            }
          }}
          className="text-muted-foreground hover:text-foreground opacity-0 transition-opacity group-hover:opacity-100 p-1"
          aria-label="Rename chat"
        >
          <PencilIcon className="size-3.5" />
        </span>
      )}
    </button>
  )
}
