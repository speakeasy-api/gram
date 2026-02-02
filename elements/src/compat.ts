/**
 * React compatibility shims for React 16.8+
 *
 * This module polyfills React 18 APIs that are used by transitive dependencies
 * (zustand, @assistant-ui/react, @tanstack/react-query) so that elements can
 * run on older React versions.
 *
 * Must be imported before any other modules that depend on these APIs.
 *
 * Based on: https://www.assistant-ui.com/docs/react-compatibility
 */

import * as React from 'react'

import { createShims } from './compat-shims'

// Cast to mutable record for patching
const ReactMutable = React as Record<string, unknown>
const shims = createShims(React)

if (typeof ReactMutable.useSyncExternalStore !== 'function') {
  ReactMutable.useSyncExternalStore = shims.useSyncExternalStore
}

if (typeof ReactMutable.useId !== 'function') {
  ReactMutable.useId = shims.useId
}

if (typeof ReactMutable.useInsertionEffect !== 'function') {
  ReactMutable.useInsertionEffect = shims.useInsertionEffect
}
