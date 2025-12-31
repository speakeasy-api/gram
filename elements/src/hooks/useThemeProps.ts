import { useMemo } from 'react'
import { useElements } from './useElements'

/**
 * Hook to get theme-related props including dark mode class
 */
export const useThemeProps = () => {
  const { config } = useElements()
  const theme = config.theme ?? {}

  return useMemo(() => {
    const { colorScheme = 'light' } = theme

    const isDark =
      colorScheme === 'dark' ||
      (colorScheme === 'system' &&
        typeof window !== 'undefined' &&
        window.matchMedia('(prefers-color-scheme: dark)').matches)

    return {
      className: isDark ? 'dark' : undefined,
    } as const
  }, [theme])
}
