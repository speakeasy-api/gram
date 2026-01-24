import type { Plugin } from '@/types/plugins'
import { chart } from './chart'
import { generativeUI } from './generative-ui'

export const recommended: Plugin[] = [chart, generativeUI]
export { chart } from './chart'
export { generativeUI } from './generative-ui'

export type { Plugin } from '@/types/plugins'
