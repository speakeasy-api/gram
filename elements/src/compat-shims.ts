/**
 * Polyfill factories for React 18 APIs. Shared by compat.ts (runtime patching)
 * and react-shim.ts (bundler-level replacement). This module must NOT import
 * from 'react' to avoid circular dependencies when the Vite plugin is active.
 */

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

function snapshotChanged<T>(inst: { value: T; getSnapshot: () => T }): boolean {
  try {
    return !Object.is(inst.value, inst.getSnapshot())
  } catch {
    return true
  }
}

function createUseSyncExternalStoreShim(R: ReactLike) {
  return function useSyncExternalStore<T>(
    subscribe: (cb: () => void) => () => void,
    getSnapshot: () => T
  ): T {
    const value = getSnapshot()
    const [{ inst }, forceUpdate] = R.useState({ inst: { value, getSnapshot } })

    R.useLayoutEffect(() => {
      inst.value = value
      inst.getSnapshot = getSnapshot
      if (snapshotChanged(inst)) forceUpdate({ inst })
    }, [subscribe, value, getSnapshot])

    R.useEffect(() => {
      if (snapshotChanged(inst)) forceUpdate({ inst })
      return subscribe(() => {
        if (snapshotChanged(inst)) forceUpdate({ inst })
      })
    }, [subscribe])

    return value
  }
}

function createUseIdShim(R: ReactLike) {
  let counter = 0
  return function useId(): string {
    const ref = R.useRef<string | null>(null)
    if (ref.current === null) ref.current = `:r${counter++}:`
    return ref.current
  }
}

/** Build polyfills for a React instance. Native APIs take precedence via ??. */
export function createShims(R: ReactLike) {
  return {
    useSyncExternalStore:
      R.useSyncExternalStore ?? createUseSyncExternalStoreShim(R),
    useId: R.useId ?? createUseIdShim(R),
    useInsertionEffect: R.useInsertionEffect ?? R.useLayoutEffect,
    startTransition: R.startTransition ?? ((cb: () => void) => cb()),
    useTransition:
      R.useTransition ??
      ((): [boolean, (cb: () => void) => void] => [false, (cb) => cb()]),
    useDeferredValue: R.useDeferredValue ?? (<T>(value: T): T => value),
  }
}
