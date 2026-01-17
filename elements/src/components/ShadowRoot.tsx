'use client'

import { useEffect, useMemo, useRef, useState, type CSSProperties, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { PortalContainerProvider } from '@/contexts/portal-container'
import { useElements } from '@/hooks/useElements'
import { useThemeProps } from '@/hooks/useThemeProps'
import { cn } from '@/lib/utils'
import { ROOT_SELECTOR } from '@/constants/tailwind'
import elementsStyles from '@/global.css?inline'

interface ShadowRootProps {
  children: ReactNode
  hostClassName?: string
  hostStyle?: CSSProperties
}

export const ShadowRoot = ({
  children,
  hostClassName,
  hostStyle,
}: ShadowRootProps) => {
  const hostRef = useRef<HTMLDivElement>(null)
  const containerRef = useRef<HTMLDivElement>(null)
  const [shadowRoot, setShadowRoot] = useState<ShadowRoot | null>(null)
  const { config } = useElements()
  const themeProps = useThemeProps()

  const rootClassName = useMemo(
    () => cn(ROOT_SELECTOR, themeProps.className),
    [themeProps.className]
  )

  useEffect(() => {
    const host = hostRef.current
    if (!host) {
      return
    }

    const root = host.shadowRoot ?? host.attachShadow({ mode: 'open' })
    setShadowRoot(root)
  }, [])

  useEffect(() => {
    if (!shadowRoot) {
      return
    }

    const existingStyle = shadowRoot.querySelector<HTMLStyleElement>(
      'style[data-gram-elements]'
    )

    if (existingStyle) {
      existingStyle.textContent = elementsStyles
      return
    }

    const styleElement = document.createElement('style')
    styleElement.setAttribute('data-gram-elements', 'true')
    styleElement.textContent = elementsStyles
    shadowRoot.prepend(styleElement)
  }, [shadowRoot, elementsStyles])

  return (
    <div ref={hostRef} className={hostClassName} style={hostStyle}>
      {shadowRoot
        ? createPortal(
            <div
              ref={containerRef}
              className={rootClassName}
              data-radius={config.theme?.radius}
              style={{ height: '100%', width: '100%' }}
            >
              <PortalContainerProvider containerRef={containerRef}>
                {children}
              </PortalContainerProvider>
            </div>,
            shadowRoot
          )
        : null}
    </div>
  )
}
