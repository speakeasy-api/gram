/**
 * Shared test app component.
 *
 * Renders ElementsProvider with a session endpoint to verify the compat
 * shims work on older React versions.
 */
import React from 'react'
import { ElementsProvider, Chat } from '@gram-ai/elements'
import '@gram-ai/elements/elements.css'

export function App() {
  return (
    <ElementsProvider
      config={{
        projectSlug: import.meta.env.VITE_GRAM_PROJECT_SLUG ?? 'test',
        mcp: import.meta.env.VITE_GRAM_MCP,
      }}
    >
      <div style={{ height: '100vh', display: 'flex', flexDirection: 'column' }}>
        <div style={{ padding: '12px 16px', borderBottom: '1px solid #e5e7eb' }}>
          <strong>Elements Integration Test</strong> — React {React.version}
        </div>
        <div style={{ flex: 1 }}>
          <Chat />
        </div>
      </div>
    </ElementsProvider>
  )
}
