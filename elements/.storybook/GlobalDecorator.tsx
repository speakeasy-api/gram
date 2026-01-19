import merge from 'lodash.merge'
import React, { useMemo } from 'react'
import { ElementsProvider } from '../src/contexts/ElementsProvider'
import { recommended } from '../src/plugins'
import { ElementsConfig } from '../src/types'
import { ROOT_SELECTOR } from '../src/constants/tailwind'

interface ElementsDecoratorProps {
  children: React.ReactNode
  // Partial so stories can override only what they need
  config?: Partial<ElementsConfig>
}

// Injected via Vite' `define` config
declare const __GRAM_API_URL__: string | undefined

const DEFAULT_ELEMENTS_CONFIG: ElementsConfig = {
  projectSlug: '',
  mcp: '',
  variant: 'widget',
  welcome: {
    title: 'Hello there!',
    subtitle: 'How can I help you today?',
    suggestions: [
      {
        title: 'Discover available tools',
        label: 'Find out what tools are available',
        action: 'Call all tools available',
      },
    ],
  },
  composer: {
    placeholder: 'Ask me anything...',
    attachments: true,
  },
  modal: {
    defaultOpen: true,
    expandable: true,
    defaultExpanded: true,
    title: 'Gram Elements Demo',
  },
  tools: {
    expandToolGroupsByDefault: true,
  },
  plugins: recommended,
  api: {
    url: __GRAM_API_URL__ || 'https://api.getgram.ai',
  },
}

/**
 * Global decorator that wraps all stories in the AssistantRuntimeProvider,
 * which provides the chat runtime to the story.
 * Note: This assumes that all stories require a chat runtime, but we move back to
 * per story decorator in the future.
 * @param children - The children to render.
 * @returns
 */
export const ElementsDecorator: React.FC<ElementsDecoratorProps> = ({
  children,
  config,
}) => {
  const finalConfig = useMemo(
    () => merge({}, DEFAULT_ELEMENTS_CONFIG, config ?? {}),
    [config]
  )

  if (!finalConfig.projectSlug || !finalConfig.mcp) {
    return (
      <div className="bg-red-300 p-4 text-red-900">
        Please provide both projectSlug and mcp in the controls panel to view
        this story.
      </div>
    )
  }

  return (
    <div className={ROOT_SELECTOR}>
      <ElementsProvider config={finalConfig}>
        <div className="bg-background h-screen">{children}</div>
      </ElementsProvider>
    </div>
  )
}
