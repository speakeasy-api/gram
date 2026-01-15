import { useElements } from './useElements'

/**
 * Density class mappings for different UI elements
 */
const densityClasses = {
  compact: {
    // Padding - small increments (1, 1.5, 2, 2.5, 3)
    'gramel:p-xs': 'gramel:p-1',
    'gramel:p-sm': 'gramel:p-1.5',
    'gramel:p-md': 'gramel:p-2',
    'gramel:p-lg': 'gramel:p-2.5',
    'gramel:p-xl': 'gramel:p-3',
    'gramel:px-xs': 'gramel:px-1',
    'gramel:px-sm': 'gramel:px-1.5',
    'gramel:px-md': 'gramel:px-2',
    'gramel:px-lg': 'gramel:px-2.5',
    'gramel:px-xl': 'gramel:px-3',
    'gramel:py-xs': 'gramel:py-1',
    'gramel:py-sm': 'gramel:py-1.5',
    'gramel:py-md': 'gramel:py-2',
    'gramel:py-lg': 'gramel:py-2.5',
    'gramel:py-xl': 'gramel:py-3',
    // Gaps - small increments
    'gramel:gap-sm': 'gramel:gap-1',
    'gramel:gap-md': 'gramel:gap-1.5',
    'gramel:gap-lg': 'gramel:gap-2',
    'gramel:gap-xl': 'gramel:gap-2.5',
    // Heights
    'gramel:h-header': 'gramel:h-10',
    'gramel:h-input': 'gramel:min-h-10',
    // Text
    'gramel:text-base': 'gramel:text-sm',
    'gramel:text-title': 'gramel:text-xl',
    'gramel:text-subtitle': 'gramel:text-sm',
  },
  normal: {
    // Padding - medium increments (1, 2, 3, 4, 5, 6)
    'gramel:p-xs': 'gramel:p-1',
    'gramel:p-sm': 'gramel:p-2',
    'gramel:p-md': 'gramel:p-3',
    'gramel:p-lg': 'gramel:p-4',
    'gramel:p-xl': 'gramel:p-6',
    'gramel:px-xs': 'gramel:px-1',
    'gramel:px-sm': 'gramel:px-2',
    'gramel:px-md': 'gramel:px-3',
    'gramel:px-lg': 'gramel:px-4',
    'gramel:px-xl': 'gramel:px-6',
    'gramel:py-xs': 'gramel:py-1',
    'gramel:py-sm': 'gramel:py-2',
    'gramel:py-md': 'gramel:py-3',
    'gramel:py-lg': 'gramel:py-4',
    'gramel:py-xl': 'gramel:py-6',
    // Gaps - medium increments
    'gramel:gap-sm': 'gramel:gap-1.5',
    'gramel:gap-md': 'gramel:gap-2',
    'gramel:gap-lg': 'gramel:gap-3',
    'gramel:gap-xl': 'gramel:gap-4',
    // Heights
    'gramel:h-header': 'gramel:h-12',
    'gramel:h-input': 'gramel:min-h-12',
    // Text
    'gramel:text-base': 'gramel:text-base',
    'gramel:text-title': 'gramel:text-2xl',
    'gramel:text-subtitle': 'gramel:text-base',
  },
  spacious: {
    // Padding - large increments (2, 3, 4, 6, 8, 10)
    'gramel:p-xs': 'gramel:p-2',
    'gramel:p-sm': 'gramel:p-3',
    'gramel:p-md': 'gramel:p-4',
    'gramel:p-lg': 'gramel:p-6',
    'gramel:p-xl': 'gramel:p-10',
    'gramel:px-xs': 'gramel:px-2',
    'gramel:px-sm': 'gramel:px-3',
    'gramel:px-md': 'gramel:px-4',
    'gramel:px-lg': 'gramel:px-6',
    'gramel:px-xl': 'gramel:px-10',
    'gramel:py-xs': 'gramel:py-2',
    'gramel:py-sm': 'gramel:py-3',
    'gramel:py-md': 'gramel:py-4',
    'gramel:py-lg': 'gramel:py-6',
    'gramel:py-xl': 'gramel:py-10',
    // Gaps - large increments
    'gramel:gap-sm': 'gramel:gap-2',
    'gramel:gap-md': 'gramel:gap-3',
    'gramel:gap-lg': 'gramel:gap-4',
    'gramel:gap-xl': 'gramel:gap-6',
    // Heights
    'gramel:h-header': 'gramel:h-14',
    'gramel:h-input': 'gramel:min-h-16',
    // Text
    'gramel:text-base': 'gramel:text-lg',
    'gramel:text-title': 'gramel:text-3xl',
    'gramel:text-subtitle': 'gramel:text-lg',
  },
} as const

type DensityToken = keyof (typeof densityClasses)['normal']

/**
 * Hook to get density classes based on theme config
 * Use: const d = useDensity(); then d('p-md') returns the appropriate padding class
 */
export const useDensity = () => {
  const { config } = useElements()
  const density = config.theme?.density ?? 'normal'

  return (token: DensityToken) => densityClasses[density][token]
}
