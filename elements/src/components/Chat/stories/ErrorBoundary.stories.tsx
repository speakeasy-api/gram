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
    <div className="gramel:my-auto gramel:flex gramel:w-full gramel:grow gramel:flex-col gramel:items-center gramel:justify-center gramel:gap-4 gramel:p-6">
      <h2 className="gramel:text-foreground gramel:text-2xl gramel:font-semibold">Hello there!</h2>
      <p className="gramel:text-muted-foreground">How can I help you today?</p>
      <p className="gramel:text-muted-foreground/60 gramel:mt-4 gramel:text-sm">
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
  onReset: () => void
  hasError: boolean
}> = ({ onTriggerError, onReset, hasError }) => (
  <div className="gramel:bg-muted/50 gramel:border-border gramel:flex gramel:items-center gramel:gap-3 gramel:border-b gramel:px-4 gramel:py-2">
    <span className="gramel:text-muted-foreground gramel:text-sm gramel:font-medium">
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
    <Button size="sm" variant="outline" onClick={onReset}>
      Reset
    </Button>
  </div>
)

// Modal variant story
export const Modal: Story = () => {
  const [shouldThrow, setShouldThrow] = useState(false)
  const [key, setKey] = useState(0)

  return (
    <ErrorTriggerProvider
      shouldThrow={shouldThrow}
      onReset={() => setShouldThrow(false)}
    >
      <div className="gramel:flex gramel:h-full gramel:w-full gramel:flex-col">
        <ErrorControls
          onTriggerError={() => setShouldThrow(true)}
          onReset={() => {
            setShouldThrow(false)
            setKey((k) => k + 1)
          }}
          hasError={shouldThrow}
        />
        <div className="gramel:flex gramel:h-full gramel:w-full gramel:flex-col gramel:gap-4 gramel:p-10">
          <h1 className="gramel:text-2xl gramel:font-bold">Modal Variant</h1>
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
  const [key, setKey] = useState(0)

  return (
    <ErrorTriggerProvider
      shouldThrow={shouldThrow}
      onReset={() => setShouldThrow(false)}
    >
      <div className="gramel:flex gramel:h-screen gramel:w-full gramel:flex-col">
        <ErrorControls
          onTriggerError={() => setShouldThrow(true)}
          onReset={() => {
            setShouldThrow(false)
            setKey((k) => k + 1)
          }}
          hasError={shouldThrow}
        />
        <div className="gramel:m-auto gramel:flex gramel:w-full gramel:max-w-3xl gramel:flex-1 gramel:flex-col">
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
  const [key, setKey] = useState(0)

  return (
    <ErrorTriggerProvider
      shouldThrow={shouldThrow}
      onReset={() => setShouldThrow(false)}
    >
      <div className="gramel:flex gramel:h-full gramel:w-full gramel:flex-col">
        <ErrorControls
          onTriggerError={() => setShouldThrow(true)}
          onReset={() => {
            setShouldThrow(false)
            setKey((k) => k + 1)
          }}
          hasError={shouldThrow}
        />
        <div className="gramel:mr-[400px] gramel:p-10">
          <h1 className="gramel:text-2xl gramel:font-bold">Sidecar Variant</h1>
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
