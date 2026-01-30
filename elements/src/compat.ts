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

// Cast to mutable record for patching
const ReactMutable = React as Record<string, unknown>

/**
 * Polyfill useSyncExternalStore (React 18+)
 *
 * Used by zustand and @tanstack/react-query. This is a simplified shim based
 * on the official `use-sync-external-store/shim` package from the React team.
 * It uses useState + useEffect to subscribe, which is safe for React 16.8+.
 */
if (typeof ReactMutable.useSyncExternalStore !== 'function') {
  ReactMutable.useSyncExternalStore = function useSyncExternalStore<T>(
    subscribe: (onStoreChange: () => void) => () => void,
    getSnapshot: () => T,
    getServerSnapshot?: () => T,
  ): T {
    // Server snapshot is only relevant for SSR with React 18's streaming renderer.
    // For older React, we always use getSnapshot.
    void getServerSnapshot

    const value = getSnapshot()
    const [{ inst }, forceUpdate] = React.useState({ inst: { value, getSnapshot } })

    React.useLayoutEffect(() => {
      inst.value = value
      inst.getSnapshot = getSnapshot

      if (!Object.is(inst.value, inst.getSnapshot())) {
        forceUpdate({ inst })
      }
    }, [subscribe, value, getSnapshot]) // eslint-disable-line react-hooks/exhaustive-deps

    React.useEffect(() => {
      if (!Object.is(inst.value, inst.getSnapshot())) {
        forceUpdate({ inst })
      }

      return subscribe(() => {
        if (!Object.is(inst.value, inst.getSnapshot())) {
          forceUpdate({ inst })
        }
      })
    }, [subscribe]) // eslint-disable-line react-hooks/exhaustive-deps

    return value
  }
}

/**
 * Polyfill useId (React 18+)
 *
 * Used by @assistant-ui/react and Radix UI primitives. Generates a stable ID
 * per component instance using useRef, matching React 18 semantics.
 */
if (typeof ReactMutable.useId !== 'function') {
  let counter = 0
  ReactMutable.useId = function useId(): string {
    const ref = React.useRef<string | null>(null)
    if (ref.current === null) {
      ref.current = `:r${counter++}:`
    }
    return ref.current
  }
}

/**
 * Polyfill useInsertionEffect (React 18+)
 *
 * Used by CSS-in-JS libraries. Falls back to useLayoutEffect which has the
 * same synchronous timing guarantees.
 */
if (typeof ReactMutable.useInsertionEffect !== 'function') {
  ReactMutable.useInsertionEffect = React.useLayoutEffect
}
