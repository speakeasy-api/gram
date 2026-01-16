import type { Meta, StoryFn } from '@storybook/react-vite'
import {
  createContext,
  useContext,
  useEffect,
  useState,
  type ComponentType,
  type FC,
} from 'react'
import { Chat } from '..'
import { Button } from '../../ui/button'

const meta: Meta<typeof Chat> = {
  title: 'Chat/ErrorBoundary',
  component: Chat,
  parameters: {
    layout: 'fullscreen',
  },
} satisfies Meta<typeof Chat>

export default meta

type Story = StoryFn<typeof Chat>

// Context to trigger errors from outside the component tree
const ErrorTriggerContext = createContext<{
  shouldThrow: boolean
  resetError: () => void
}>({
  shouldThrow: false,
  resetError: () => {},
})

// Component that throws when context says so - used to override ThreadWelcome
const ThrowingThreadWelcome: FC = () => {
  const { shouldThrow, resetError } = useContext(ErrorTriggerContext)

  // Reset the error state when this component unmounts (happens when "Try again" is clicked)
  useEffect(() => {
    return () => {
      resetError()
    }
  }, [resetError])

  if (shouldThrow) {
    throw new Error('Simulated error: Failed to load chat interface')
  }

  // Render a simple welcome that matches the default styling
  return (
    <div className="my-auto flex w-full grow flex-col items-center justify-center gap-4 p-6">
      <h2 className="text-foreground text-2xl font-semibold">Hello there!</h2>
      <p className="text-muted-foreground">How can I help you today?</p>
      <p className="text-muted-foreground/60 mt-4 text-sm">
        Click &quot;Trigger Error&quot; above to see the error boundary
      </p>
    </div>
  )
}

// Wrapper to provide the error trigger context
const ErrorTriggerProvider: FC<{
  children: React.ReactNode
  shouldThrow: boolean
  onReset: () => void
}> = ({ children, shouldThrow, onReset }) => {
  return (
    <ErrorTriggerContext.Provider value={{ shouldThrow, resetError: onReset }}>
      {children}
    </ErrorTriggerContext.Provider>
  )
}

// Control bar component for triggering errors
const ErrorControls: FC<{
  onTriggerError: () => void
  hasError: boolean
}> = ({ onTriggerError, hasError }) => (
  <div className="bg-muted/50 border-border flex items-center gap-3 border-b px-4 py-2">
    <span className="text-muted-foreground text-sm font-medium">
      Error Boundary Demo:
    </span>
    <Button
      size="sm"
      variant="destructive"
      onClick={onTriggerError}
      disabled={hasError}
    >
      Trigger Error
    </Button>
  </div>
)

// Modal variant story
export const Modal: Story = () => {
  const [shouldThrow, setShouldThrow] = useState(false)
  const [key] = useState(0)

  return (
    <ErrorTriggerProvider
      shouldThrow={shouldThrow}
      onReset={() => setShouldThrow(false)}
    >
      <div className="flex h-full w-full flex-col">
        <ErrorControls
          onTriggerError={() => setShouldThrow(true)}
          hasError={shouldThrow}
        />
        <div className="flex h-full w-full flex-col gap-4 p-10">
          <h1 className="text-2xl font-bold">Modal Variant</h1>
          <p>
            Click the button in the bottom right corner to open the chat, then
            trigger an error.
          </p>
          <Chat key={key} />
        </div>
      </div>
    </ErrorTriggerProvider>
  )
}
Modal.parameters = {
  elements: {
    config: {
      variant: 'widget',
      modal: { defaultOpen: true },
      components: {
        ThreadWelcome: ThrowingThreadWelcome as ComponentType,
      },
    },
  },
}

// Standalone variant story
export const Standalone: Story = () => {
  const [shouldThrow, setShouldThrow] = useState(false)
  const [key] = useState(0)

  return (
    <ErrorTriggerProvider
      shouldThrow={shouldThrow}
      onReset={() => setShouldThrow(false)}
    >
      <div className="flex h-screen w-full flex-col">
        <ErrorControls
          onTriggerError={() => setShouldThrow(true)}
          hasError={shouldThrow}
        />
        <div className="m-auto flex w-full max-w-3xl flex-1 flex-col">
          <Chat key={key} />
        </div>
      </div>
    </ErrorTriggerProvider>
  )
}
Standalone.parameters = {
  elements: {
    config: {
      variant: 'standalone',
      components: {
        ThreadWelcome: ThrowingThreadWelcome as ComponentType,
      },
    },
  },
}

// Sidecar variant story
export const Sidecar: Story = () => {
  const [shouldThrow, setShouldThrow] = useState(false)
  const [key] = useState(0)

  return (
    <ErrorTriggerProvider
      shouldThrow={shouldThrow}
      onReset={() => setShouldThrow(false)}
    >
      <div className="flex h-full w-full flex-col">
        <ErrorControls
          onTriggerError={() => setShouldThrow(true)}
          hasError={shouldThrow}
        />
        <div className="mr-[400px] p-10">
          <h1 className="text-2xl font-bold">Sidecar Variant</h1>
          <p>
            The sidebar is always visible on the right. Trigger an error to see
            the error boundary.
          </p>
          <Chat key={key} />
        </div>
      </div>
    </ErrorTriggerProvider>
  )
}
Sidecar.parameters = {
  elements: {
    config: {
      variant: 'sidecar',
      sidecar: {
        title: 'Error Boundary Demo',
      },
      components: {
        ThreadWelcome: ThrowingThreadWelcome as ComponentType,
      },
    },
  },
}
