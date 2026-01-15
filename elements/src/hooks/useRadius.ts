import { Radius } from '@/types'
import { useElements } from './useElements'

/**
 * Radius class mappings for different UI elements
 */
const radiusClasses: Record<Radius, Record<RadiusSize, string>> = {
  sharp: {
    sm: 'gramel:rounded-sm',
    md: 'gramel:rounded',
    lg: 'gramel:rounded-md',
    xl: 'gramel:rounded-lg',
    full: 'gramel:rounded-lg',
  },
  soft: {
    sm: 'gramel:rounded',
    md: 'gramel:rounded-lg',
    lg: 'gramel:rounded-xl',
    xl: 'gramel:rounded-2xl',
    full: 'gramel:rounded-full',
  },
  round: {
    sm: 'gramel:rounded-lg',
    md: 'gramel:rounded-xl',
    lg: 'gramel:rounded-2xl',
    xl: 'gramel:rounded-3xl',
    full: 'gramel:rounded-full',
  },
} as const

type RadiusSize = 'sm' | 'md' | 'lg' | 'xl' | 'full'

/**
 * Hook to get radius classes based on theme config
 * Use: const r = useRadius(); then r('lg') returns the appropriate gramel:rounded class
 */
export const useRadius = () => {
  const { config } = useElements()
  const radius = config.theme?.radius ?? 'soft'

  return (size: RadiusSize) => radiusClasses[radius][size]
}
