import merge from 'lodash.merge'
import React, { useEffect, useMemo, useState } from 'react'
import { ElementsProvider } from '../src/contexts/ElementsProvider'
import { recommended } from '../src/plugins'
import { ElementsConfig } from '../src/types'

interface ElementsDecoratorProps {
  children: React.ReactNode
  // Partial so stories can override only what they need
  config?: Partial<ElementsConfig>
}

const DEFAULT_ELEMENTS_CONFIG: ElementsConfig = {
  clientToken: null,
  projectSlug: 'adamtest',
  mcp: 'https://chat.speakeasy.com/mcp/speakeasy-team-my_api',
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
}

const useSession = () => {
  const [session, setSession] = useState<string | null>(null)
  useEffect(() => {
    fetch(`/session`, {
      method: 'POST',
    })
      .then((response) => response.json())
      .then((data) => {
        setSession(data.client_token)
      })
      .catch((error) => {
        console.error('Error creating session:', error)
      })
  }, [])

  return session
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
  const session = useSession()

  const finalConfig = useMemo(
    () => merge({}, DEFAULT_ELEMENTS_CONFIG, config ?? {}),
    [config]
  )

  finalConfig.clientToken = session

  return (
    <ElementsProvider config={finalConfig}>
      <div className="h-screen bg-zinc-50">{children}</div>
    </ElementsProvider>
  )
}
