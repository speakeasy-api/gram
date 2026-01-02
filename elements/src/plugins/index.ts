import type { Plugin } from '@/types/plugins'
import { chart } from './chart'

export const recommended: Plugin[] = [chart]
export { chart } from './chart'

export type { Plugin } from '@/types/plugins'
