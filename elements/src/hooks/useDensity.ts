import { useElements } from './useElements'

/**
 * Density class mappings for different UI elements
 */
const densityClasses = {
  compact: {
    // Padding - small increments (1, 1.5, 2, 2.5, 3)
    'p-xs': 'p-1',
    'p-sm': 'p-1.5',
    'p-md': 'p-2',
    'p-lg': 'p-2.5',
    'p-xl': 'p-3',
    'px-xs': 'px-1',
    'px-sm': 'px-1.5',
    'px-md': 'px-2',
    'px-lg': 'px-2.5',
    'px-xl': 'px-3',
    'py-xs': 'py-1',
    'py-sm': 'py-1.5',
    'py-md': 'py-2',
    'py-lg': 'py-2.5',
    'py-xl': 'py-3',
    // Gaps - small increments
    'gap-sm': 'gap-1',
    'gap-md': 'gap-1.5',
    'gap-lg': 'gap-2',
    'gap-xl': 'gap-2.5',
    // Heights
    'h-header': 'h-10',
    'h-input': 'min-h-10',
    // Text
    'text-base': 'text-sm',
    'text-title': 'text-xl',
    'text-subtitle': 'text-sm',
  },
  normal: {
    // Padding - medium increments (1, 2, 3, 4, 5, 6)
    'p-xs': 'p-1',
    'p-sm': 'p-2',
    'p-md': 'p-3',
    'p-lg': 'p-4',
    'p-xl': 'p-6',
    'px-xs': 'px-1',
    'px-sm': 'px-2',
    'px-md': 'px-3',
    'px-lg': 'px-4',
    'px-xl': 'px-6',
    'py-xs': 'py-1',
    'py-sm': 'py-2',
    'py-md': 'py-3',
    'py-lg': 'py-4',
    'py-xl': 'py-6',
    // Gaps - medium increments
    'gap-sm': 'gap-1.5',
    'gap-md': 'gap-2',
    'gap-lg': 'gap-3',
    'gap-xl': 'gap-4',
    // Heights
    'h-header': 'h-12',
    'h-input': 'min-h-12',
    // Text
    'text-base': 'text-base',
    'text-title': 'text-2xl',
    'text-subtitle': 'text-base',
  },
  spacious: {
    // Padding - large increments (2, 3, 4, 6, 8, 10)
    'p-xs': 'p-2',
    'p-sm': 'p-3',
    'p-md': 'p-4',
    'p-lg': 'p-6',
    'p-xl': 'p-10',
    'px-xs': 'px-2',
    'px-sm': 'px-3',
    'px-md': 'px-4',
    'px-lg': 'px-6',
    'px-xl': 'px-10',
    'py-xs': 'py-2',
    'py-sm': 'py-3',
    'py-md': 'py-4',
    'py-lg': 'py-6',
    'py-xl': 'py-10',
    // Gaps - large increments
    'gap-sm': 'gap-2',
    'gap-md': 'gap-3',
    'gap-lg': 'gap-4',
    'gap-xl': 'gap-6',
    // Heights
    'h-header': 'h-14',
    'h-input': 'min-h-16',
    // Text
    'text-base': 'text-lg',
    'text-title': 'text-3xl',
    'text-subtitle': 'text-lg',
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
