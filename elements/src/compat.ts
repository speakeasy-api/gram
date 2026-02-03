/**
 * Runtime React 16/17 compatibility shims.
 *
 * Patches the React module object with polyfills for React 18 APIs used by
 * transitive deps (zustand, @assistant-ui/react, @tanstack/react-query).
 * Must be imported before any modules that depend on these APIs.
 */

import * as React from 'react'
import { createShims } from './compat-shims'

const ReactMutable = React as Record<string, unknown>
const shims = createShims(React)

for (const [key, impl] of Object.entries(shims)) {
  if (typeof ReactMutable[key] !== 'function') {
    ReactMutable[key] = impl
  }
}
