import { DocsContainer } from '@storybook/addon-docs/blocks'
import React, { useEffect, useState } from 'react'
import { themes } from 'storybook/theming'

import { addons } from 'storybook/preview-api'

const DARK_MODE_EVENT_NAME = 'DARK_MODE'

function useDocumentClassListObserver(
  callback: (classList: DOMTokenList) => void
) {
  useEffect(() => {
    // Define a MutationObserver that listens to changes in classList
    const observer = new MutationObserver((mutationsList) => {
      for (const mutation of mutationsList) {
        if (mutation.attributeName === 'class') {
          callback(document.documentElement.classList)
        }
      }
    })

    // Start observing the documentElement for attribute changes
    observer.observe(document.documentElement, { attributes: true })

    // Clean up on component unmount
    return () => observer.disconnect()
  }, [callback])
}

export const ThemedDocsContainer = ({ children, context }) => {
  const [isDark, setIsDark] = useState(
    document.documentElement.classList.contains('dark')
  )

  useDocumentClassListObserver((classList) => {
    setIsDark(classList.contains('dark'))
  })
  useEffect(() => {
    const chan = addons.getChannel()
    chan.on(DARK_MODE_EVENT_NAME, setIsDark)
    return () => chan.off(DARK_MODE_EVENT_NAME, setIsDark)
  }, [])

  return (
    <DocsContainer
      context={context}
      theme={isDark ? themes.dark : themes.light}
    >
      {children}
    </DocsContainer>
  )
}
