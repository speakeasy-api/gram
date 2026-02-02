/**
 * Shared polyfill implementations for React 18 APIs.
 *
 * Used by both:
 * - compat.ts (runtime patching of the React module object)
 * - react-shim.ts (bundler-level module replacement via the Vite plugin)
 *
 * Each factory takes a React-like object so the caller can pass either
 * `import * as React from 'react'` or `import ReactOriginal from 'react-original'`.
 */

// Minimal interface for the React hooks we depend on
interface ReactLike {
  useState: typeof import('react').useState
  useEffect: typeof import('react').useEffect
  useLayoutEffect: typeof import('react').useLayoutEffect
  useRef: typeof import('react').useRef
  useSyncExternalStore?: typeof import('react').useSyncExternalStore
  useId?: typeof import('react').useId
  useInsertionEffect?: typeof import('react').useInsertionEffect
  startTransition?: typeof import('react').startTransition
  useTransition?: typeof import('react').useTransition
  useDeferredValue?: typeof import('react').useDeferredValue
}

/**
 * Check if a snapshot has changed, catching errors from getSnapshot().
 * Matches the official use-sync-external-store/shim behavior where errors
 * are treated as "changed" to trigger a re-render.
 */
function snapshotChanged<T>(inst: { value: T; getSnapshot: () => T }): boolean {
  try {
    return !Object.is(inst.value, inst.getSnapshot())
  } catch {
    return true
  }
}

/**
 * Polyfill useSyncExternalStore (React 18+)
 *
 * Used by zustand and @tanstack/react-query. Simplified shim based on the
 * official `use-sync-external-store/shim` package from the React team.
 */
export function createUseSyncExternalStoreShim(React: ReactLike) {
  return function useSyncExternalStore<T>(
    subscribe: (onStoreChange: () => void) => () => void,
    getSnapshot: () => T,
    getServerSnapshot?: () => T
  ): T {
    void getServerSnapshot

    const value = getSnapshot()
    const [{ inst }, forceUpdate] = React.useState({
      inst: { value, getSnapshot },
    })

    React.useLayoutEffect(() => {
      inst.value = value
      inst.getSnapshot = getSnapshot

      if (snapshotChanged(inst)) {
        forceUpdate({ inst })
      }
    }, [subscribe, value, getSnapshot])

    React.useEffect(() => {
      if (snapshotChanged(inst)) {
        forceUpdate({ inst })
      }

      return subscribe(() => {
        if (snapshotChanged(inst)) {
          forceUpdate({ inst })
        }
      })
    }, [subscribe])

    return value
  }
}

/**
 * Polyfill useId (React 18+)
 *
 * Used by @assistant-ui/react and Radix UI primitives. Generates a stable
 * ID per component instance using useRef.
 */
export function createUseIdShim(React: ReactLike) {
  let counter = 0
  return function useId(): string {
    const ref = React.useRef<string | null>(null)
    if (ref.current === null) {
      ref.current = `:r${counter++}:`
    }
    return ref.current
  }
}

/**
 * Build the complete set of shims for a given React instance.
 * Existing native implementations take precedence.
 */
export function createShims(React: ReactLike) {
  return {
    useSyncExternalStore:
      React.useSyncExternalStore ?? createUseSyncExternalStoreShim(React),
    useId: React.useId ?? createUseIdShim(React),
    useInsertionEffect: React.useInsertionEffect ?? React.useLayoutEffect,
    startTransition: React.startTransition ?? ((cb: () => void) => cb()),
    useTransition:
      React.useTransition ??
      ((): [boolean, (cb: () => void) => void] => [false, (cb) => cb()]),
    useDeferredValue: React.useDeferredValue ?? (<T>(value: T): T => value),
  }
}
