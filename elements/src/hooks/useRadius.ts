import { Radius } from '@/types'
import { useElements } from './useElements'

/**
 * Radius class mappings for different UI elements
 */
const radiusClasses: Record<Radius, Record<RadiusSize, string>> = {
  sharp: {
    sm: 'rounded-sm',
    md: 'rounded',
    lg: 'rounded-md',
    xl: 'rounded-lg',
    full: 'rounded-lg',
  },
  soft: {
    sm: 'rounded',
    md: 'rounded-lg',
    lg: 'rounded-xl',
    xl: 'rounded-2xl',
    full: 'rounded-full',
  },
  round: {
    sm: 'rounded-lg',
    md: 'rounded-xl',
    lg: 'rounded-2xl',
    xl: 'rounded-3xl',
    full: 'rounded-full',
  },
} as const

type RadiusSize = 'sm' | 'md' | 'lg' | 'xl' | 'full'

/**
 * Hook to get radius classes based on theme config
 * Use: const r = useRadius(); then r('lg') returns the appropriate rounded class
 */
export const useRadius = () => {
  const { config } = useElements()
  const radius = config.theme?.radius ?? 'soft'

  return (size: RadiusSize) => radiusClasses[radius][size]
}
