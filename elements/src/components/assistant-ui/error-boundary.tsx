'use client'

import { AlertCircle } from 'lucide-react'
import { Component, type ErrorInfo, type ReactNode } from 'react'
import { trackError } from '@/lib/errorTracking'
import { cn } from '@/lib/utils'
import { Button } from '../ui/button'

interface ErrorBoundaryProps {
  children: ReactNode
  fallback?: ReactNode
  onError?: (error: Error, errorInfo: ErrorInfo) => void
  onReset?: () => void
}

interface ErrorBoundaryState {
  hasError: boolean
  error: Error | null
  resetKey: number
}

interface ErrorFallbackProps {
  error: Error | null
  onRetry: () => void
}

// eslint-disable-next-line react-refresh/only-export-components
const ErrorFallback = ({ error, onRetry }: ErrorFallbackProps) => {
  return (
    <div
      className={cn('aui-root aui-error-boundary gramel:bg-background gramel:flex gramel:h-full gramel:w-full gramel:flex-col gramel:items-center gramel:justify-center gramel:p-6'
      )}
    >
      <div className="gramel:flex gramel:flex-col gramel:items-center gramel:gap-4 gramel:text-center">
        <div className="gramel:text-destructive">
          <AlertCircle className="gramel:size-12 gramel:stroke-[1.5px]" />
        </div>
        <div className="gramel:flex gramel:flex-col gramel:gap-2">
          <h3 className="gramel:text-foreground gramel:text-xl gramel:font-semibold">
            Something went wrong
          </h3>
          <p className="gramel:text-muted-foreground gramel:text-base">
            An error occurred while loading the chat.
          </p>
          {error && (
            <p className="gramel:text-muted-foreground/60 gramel:max-w-md gramel:truncate gramel:text-sm">
              {error.message}
            </p>
          )}
        </div>
        <Button onClick={onRetry} variant="default" className="gramel:mt-2">
          Try again
        </Button>
      </div>
    </div>
  )
}

// eslint-disable-next-line react-refresh/only-export-components
const Remounter = ({ children }: { children: ReactNode }) => <>{children}</>

/**
 * Global error boundary for the Elements library. Catches unexpected errors and renders a fallback UI.
 * We wrap the assistant-modal, assistant-sidecar, and chat components with this error boundary.
 * Each variant needs to have the error boundary rendered at the appropriate level e.g if using
 * the widget variant, then the error screen must be rendered within the widget modal.
 * TODO: We should add more granular error boundaries (e.g wrapping AssistantMessage, ThreadWelcome, etc.)
 * TODO: We should also wrap ChatHistory, which may yield its own errors.
 */
export class ErrorBoundary extends Component<
  ErrorBoundaryProps,
  ErrorBoundaryState
> {
  constructor(props: ErrorBoundaryProps) {
    super(props)
    this.state = { hasError: false, error: null, resetKey: 0 }
  }

  static getDerivedStateFromError(error: Error): Partial<ErrorBoundaryState> {
    return { hasError: true, error }
  }

  componentDidCatch(error: Error, errorInfo: ErrorInfo): void {
    // Track error to Datadog RUM
    trackError(error, {
      source: 'error-boundary',
      componentStack: errorInfo.componentStack ?? undefined,
    })
    this.props.onError?.(error, errorInfo)
  }

  handleRetry = () => {
    // Increment resetKey to force remount of children, reinitializing the chat
    this.setState((state) => ({
      hasError: false,
      error: null,
      resetKey: state.resetKey + 1,
    }))
    this.props.onReset?.()
  }

  render(): ReactNode {
    if (this.state.hasError) {
      if (this.props.fallback) {
        return this.props.fallback
      }

      return (
        <ErrorFallback error={this.state.error} onRetry={this.handleRetry} />
      )
    }

    // Use Remounter with key to force unmount/remount of children when retry is clicked
    return (
      <Remounter key={this.state.resetKey}>{this.props.children}</Remounter>
    )
  }
}
