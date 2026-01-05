import {
  CodeHeaderProps,
  SyntaxHighlighterProps,
} from '@assistant-ui/react-markdown'
import { ComponentType, useMemo } from 'react'
import { Plugin } from '@/types/plugins'

type ComponentsByLanguage =
  | Record<
      string,
      {
        CodeHeader?: ComponentType<CodeHeaderProps> | undefined
        SyntaxHighlighter?: ComponentType<SyntaxHighlighterProps> | undefined
      }
    >
  | undefined

export function useComponentsByLanguage(plugins: Plugin[]) {
  return useMemo(() => {
    return plugins.reduce((acc, plugin) => {
      if (acc?.[plugin.language] && !plugin.overrideExisting) {
        return acc
      }
      acc = {
        ...acc,
        [plugin.language]: {
          CodeHeader: plugin.Header ?? (() => null),
          SyntaxHighlighter: plugin.Component ?? undefined,
        },
      }
      return acc
    }, {} as ComponentsByLanguage)
  }, [plugins])
}
