'use client'

import * as React from 'react'
import {
  Tabs as TabsPrimitive,
  TabsList,
  TabsTrigger,
  TabsContent as TabsContentPrimitive,
} from './tabs'

export interface TabsWrapperProps {
  defaultValue?: string
  tabs: Array<{ value: string; label: string }>
  children?: React.ReactNode
}

/**
 * Tabs wrapper that takes a tabs array and renders the tab list automatically.
 */
export function TabsWrapper({
  defaultValue,
  tabs,
  children,
}: TabsWrapperProps) {
  const defaultTab = defaultValue ?? tabs[0]?.value

  return (
    <TabsPrimitive defaultValue={defaultTab}>
      <TabsList>
        {tabs.map((tab) => (
          <TabsTrigger key={tab.value} value={tab.value}>
            {tab.label}
          </TabsTrigger>
        ))}
      </TabsList>
      {children}
    </TabsPrimitive>
  )
}

export interface TabContentWrapperProps {
  value: string
  children?: React.ReactNode
}

/**
 * TabContent wrapper - passes through to TabsContent.
 */
export function TabContentWrapper({ value, children }: TabContentWrapperProps) {
  return <TabsContentPrimitive value={value}>{children}</TabsContentPrimitive>
}
