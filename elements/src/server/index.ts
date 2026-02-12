/**
 * Server adapters for Gram Elements.
 *
 * @module
 *
 * This module re-exports all server adapters. However, it's recommended to import
 * directly from the specific adapter modules to avoid bundling unused frameworks:
 *
 * ```typescript
 * // Recommended (tree-shakeable)
 * import { createExpressHandler } from '@gram-ai/elements/server/express'
 *
 * // Not recommended (bundles all adapters)
 * import { createExpressHandler } from '@gram-ai/elements/server'
 * ```
 */

export type { SessionHandlerOptions } from './core'
export { createExpressHandler } from './express'
export { createNextHandler } from './nextjs'
export { createFastifyHandler } from './fastify'
export { createHonoHandler } from './hono'
