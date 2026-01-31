import React, { useState } from 'react'
import type { Meta, StoryFn } from '@storybook/react-vite'
import { Zap, Globe, Settings, Palette } from 'lucide-react'
import { CommandBar } from '..'
import { CommandBarProvider } from '../../../contexts/CommandBarContext'
import { useCommandBarActions } from '../../../hooks/useCommandBarActions'
import { useCommandBar } from '../../../contexts/CommandBarContext'
import type {
  CommandBarAction,
  CommandBarConfig,
  CommandBarActionEvent,
} from '../../../types'

/**
 * Wrapper that provides CommandBarProvider for stories.
 * The global decorator provides ElementsProvider, and we layer
 * CommandBarProvider on top of it.
 */
function CommandBarStoryWrapper({
  config,
  children,
}: {
  config?: CommandBarConfig
  children?: React.ReactNode
}) {
  return (
    <CommandBarProvider config={config}>
      <CommandBar />
      {children}
    </CommandBarProvider>
  )
}

const meta: Meta = {
  title: 'CommandBar',
  parameters: {
    layout: 'fullscreen',
  },
}

export default meta

// ─────────────────────────────────────────────
// Basic: Default — actions auto-surfaced from MCP / frontend tools
// ─────────────────────────────────────────────

export const Default: StoryFn = () => {
  return (
    <CommandBarStoryWrapper
      config={{
        onAction: (event: CommandBarActionEvent) => {
          console.log('Action executed:', event.action.label, event)
        },
      }}
    >
      <StoryPrompt>
        Press <Kbd>⌘K</Kbd> to open the command bar. Actions are
        auto-populated from discovered MCP and frontend tools.
      </StoryPrompt>
    </CommandBarStoryWrapper>
  )
}

// ─────────────────────────────────────────────
// Open by default (for visual testing)
// ─────────────────────────────────────────────

function AutoOpenCommandBar({ config }: { config?: CommandBarConfig }) {
  return (
    <CommandBarProvider config={config}>
      <CommandBar />
      <AutoOpen />
    </CommandBarProvider>
  )
}

function AutoOpen() {
  const { open, isOpen } = useCommandBar()
  React.useEffect(() => {
    if (!isOpen) {
      // Small delay to ensure provider is ready
      const t = setTimeout(open, 100)
      return () => clearTimeout(t)
    }
  }, [open, isOpen])
  return null
}

export const OpenByDefault: StoryFn = () => {
  return <AutoOpenCommandBar />
}
OpenByDefault.storyName = 'Open by Default'

// ─────────────────────────────────────────────
// Empty state (shows AI fallback hint)
// ─────────────────────────────────────────────

export const EmptyState: StoryFn = () => {
  return (
    <AutoOpenCommandBar
      config={{
        actions: [],
        placeholder: 'Ask me anything...',
      }}
    />
  )
}
EmptyState.storyName = 'Empty State (AI Fallback)'

// ─────────────────────────────────────────────
// Custom shortcut
// ─────────────────────────────────────────────

export const CustomShortcut: StoryFn = () => {
  return (
    <CommandBarStoryWrapper
      config={{
        shortcut: 'ctrl+shift+p',
        placeholder: 'Search commands...',
      }}
    >
      <StoryPrompt>
        Press <Kbd>Ctrl+Shift+P</Kbd> to open (VS Code style)
      </StoryPrompt>
    </CommandBarStoryWrapper>
  )
}
CustomShortcut.storyName = 'Custom Shortcut (Ctrl+Shift+P)'

// ─────────────────────────────────────────────
// Fire and forget disabled (shows loading state)
// ─────────────────────────────────────────────

export const WithLoadingState: StoryFn = () => {
  const actions: CommandBarAction[] = [
    {
      id: 'slow-action',
      label: 'Slow Action (2s)',
      description: 'This action takes 2 seconds to complete',
      icon: <Zap className="size-4" />,
      group: 'Actions',
      onSelect: () =>
        new Promise((resolve) => {
          console.log('Starting slow action...')
          setTimeout(() => {
            console.log('Slow action complete!')
            resolve('done')
          }, 2000)
        }),
    },
    {
      id: 'failing-action',
      label: 'Failing Action',
      description: 'This action will fail with an error',
      icon: <Zap className="size-4" />,
      group: 'Actions',
      onSelect: () =>
        new Promise((_, reject) => {
          setTimeout(() => reject(new Error('Something went wrong')), 1000)
        }),
    },
  ]

  return (
    <AutoOpenCommandBar
      config={{
        fireAndForget: false,
        actions,
        onAction: (event) => {
          console.log('Action result:', event)
        },
      }}
    />
  )
}
WithLoadingState.storyName = 'Fire-and-Forget Disabled'

// ─────────────────────────────────────────────
// Dynamic actions via useCommandBarActions hook
// ─────────────────────────────────────────────

function DynamicActionsDemo() {
  const [count, setCount] = useState(0)

  useCommandBarActions(
    [
      {
        id: 'increment',
        label: `Increment Counter (current: ${count})`,
        icon: <Zap className="size-4" />,
        group: 'Dynamic',
        onSelect: () => setCount((c) => c + 1),
      },
      {
        id: 'reset',
        label: 'Reset Counter',
        group: 'Dynamic',
        onSelect: () => setCount(0),
      },
    ],
    [count]
  )

  return (
    <StoryPrompt>
      Counter: <strong>{count}</strong> — Press <Kbd>⌘K</Kbd> to increment via
      command bar
    </StoryPrompt>
  )
}

export const DynamicActions: StoryFn = () => {
  return (
    <CommandBarStoryWrapper>
      <DynamicActionsDemo />
    </CommandBarStoryWrapper>
  )
}
DynamicActions.storyName = 'Dynamic Actions (useCommandBarActions)'

// ─────────────────────────────────────────────
// Many actions (scroll behavior)
// ─────────────────────────────────────────────

export const ManyActions: StoryFn = () => {
  const manyActions: CommandBarAction[] = Array.from({ length: 30 }, (_, i) => ({
    id: `action-${i}`,
    label: `Action ${i + 1}`,
    description: `Description for action ${i + 1}`,
    group: `Group ${Math.floor(i / 5) + 1}`,
    onSelect: () => console.log(`Action ${i + 1} selected`),
  }))

  return (
    <AutoOpenCommandBar
      config={{
        actions: manyActions,
        maxVisible: 8,
      }}
    />
  )
}
ManyActions.storyName = 'Many Actions (Scroll)'

// ─────────────────────────────────────────────
// Disabled actions
// ─────────────────────────────────────────────

export const DisabledActions: StoryFn = () => {
  const actions: CommandBarAction[] = [
    {
      id: 'enabled',
      label: 'Enabled Action',
      icon: <Zap className="size-4" />,
      group: 'Actions',
      onSelect: () => console.log('Enabled action!'),
    },
    {
      id: 'disabled-1',
      label: 'Disabled Action (no permission)',
      description: 'You need admin access',
      icon: <Settings className="size-4" />,
      group: 'Actions',
      disabled: true,
      onSelect: () => console.log('Should not fire'),
    },
    {
      id: 'disabled-2',
      label: 'Disabled Action (coming soon)',
      description: 'Feature not available yet',
      icon: <Palette className="size-4" />,
      group: 'Actions',
      disabled: true,
      onSelect: () => console.log('Should not fire'),
    },
  ]

  return (
    <AutoOpenCommandBar config={{ actions }} />
  )
}

// ─────────────────────────────────────────────
// Programmatic open/close
// ─────────────────────────────────────────────

function ProgrammaticDemo() {
  const { open, close, isOpen, toggle } = useCommandBar()

  return (
    <div className="flex gap-2 p-8">
      <button
        type="button"
        onClick={open}
        className="bg-primary text-primary-foreground rounded px-3 py-1.5 text-sm"
      >
        Open
      </button>
      <button
        type="button"
        onClick={close}
        className="bg-muted text-foreground rounded px-3 py-1.5 text-sm"
      >
        Close
      </button>
      <button
        type="button"
        onClick={toggle}
        className="bg-muted text-foreground rounded px-3 py-1.5 text-sm"
      >
        Toggle
      </button>
      <span className="text-muted-foreground self-center text-sm">
        Status: {isOpen ? 'Open' : 'Closed'}
      </span>
    </div>
  )
}

export const Programmatic: StoryFn = () => {
  return (
    <CommandBarStoryWrapper>
      <ProgrammaticDemo />
    </CommandBarStoryWrapper>
  )
}
Programmatic.storyName = 'Programmatic Control'

// ─────────────────────────────────────────────
// With Chat (combined usage)
// ─────────────────────────────────────────────

export const WithChat: StoryFn = () => {
  // Import Chat dynamically to avoid circular dependency in stories
  const ChatComponent = React.lazy(() =>
    import('../../Chat').then((mod) => ({ default: mod.Chat }))
  )

  return (
    <CommandBarStoryWrapper>
      <StoryPrompt>
        Press <Kbd>⌘K</Kbd> for command bar. Chat widget is also available.
        Tool actions are auto-surfaced from MCP / frontend tools.
      </StoryPrompt>
      <React.Suspense fallback={null}>
        <ChatComponent />
      </React.Suspense>
    </CommandBarStoryWrapper>
  )
}
WithChat.storyName = 'Combined with Chat'

// ─────────────────────────────────────────────
// Event callbacks
// ─────────────────────────────────────────────

function EventLog() {
  const [events, setEvents] = useState<string[]>([])

  useCommandBarActions([
    {
      id: 'sync-action',
      label: 'Sync Action',
      icon: <Zap className="size-4" />,
      group: 'Actions',
      onSelect: () => {
        setEvents((prev) => [...prev, `[${new Date().toLocaleTimeString()}] Sync action executed`])
      },
    },
    {
      id: 'async-action',
      label: 'Async Action (1s)',
      icon: <Globe className="size-4" />,
      group: 'Actions',
      onSelect: async () => {
        await new Promise((resolve) => setTimeout(resolve, 1000))
        setEvents((prev) => [...prev, `[${new Date().toLocaleTimeString()}] Async action resolved`])
        return 'success'
      },
    },
  ])

  return (
    <div className="p-8">
      <p className="text-muted-foreground mb-4 text-sm">
        Open the command bar and select actions. Events are logged below.
      </p>
      <div className="bg-muted max-h-48 overflow-y-auto rounded p-3 font-mono text-xs">
        {events.length === 0 ? (
          <span className="text-muted-foreground">No events yet...</span>
        ) : (
          events.map((e, i) => (
            <div key={i} className="text-foreground py-0.5">{e}</div>
          ))
        )}
      </div>
    </div>
  )
}

export const EventCallbacks: StoryFn = () => {
  return (
    <CommandBarStoryWrapper
      config={{
        onAction: (event) => {
          console.log('onAction callback:', event)
        },
      }}
    >
      <EventLog />
    </CommandBarStoryWrapper>
  )
}
EventCallbacks.storyName = 'Event Callbacks'

// ─────────────────────────────────────────────
// Search / Filtering demo
// ─────────────────────────────────────────────

export const SearchFiltering: StoryFn = () => {
  return (
    <AutoOpenCommandBar
      config={{
        placeholder: 'Search tools by name or keyword...',
      }}
    />
  )
}
SearchFiltering.storyName = 'Search & Keyword Filtering'

// ─────────────────────────────────────────────
// Standalone (no ElementsProvider)
// ─────────────────────────────────────────────

export const Standalone: StoryFn = () => {
  return (
    <CommandBarStoryWrapper
      config={{
        placeholder: 'Standalone mode — no AI or tools',
      }}
    >
      <StoryPrompt>
        Standalone mode (no ElementsProvider). AI fallback and tool
        auto-surfacing are unavailable. Press <Kbd>⌘K</Kbd> to open.
      </StoryPrompt>
    </CommandBarStoryWrapper>
  )
}
Standalone.parameters = {
  elements: {
    skipProvider: true,
  },
}
Standalone.storyName = 'Standalone (No ElementsProvider)'

// ─────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────

function StoryPrompt({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex h-screen items-center justify-center">
      <p className="text-muted-foreground text-sm">{children}</p>
    </div>
  )
}

function Kbd({ children }: { children: React.ReactNode }) {
  return (
    <kbd className="bg-muted text-foreground rounded px-1.5 py-0.5 text-xs font-medium">
      {children}
    </kbd>
  )
}
