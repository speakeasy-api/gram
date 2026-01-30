/**
 * React shim for older versions (16.8+, 17).
 *
 * Named imports like `import { useId } from 'react'` bind at module load
 * time, so runtime patching (compat.ts) alone cannot inject missing APIs.
 * This shim re-exports everything from the real React module and adds
 * polyfills for APIs introduced in React 18.
 *
 * Consumers must use the reactShimPlugin() in their Vite config.
 */

// @ts-expect-error — resolved by the Vite plugin to the real react package
import ReactOriginal from 'react-original'

// ---- Polyfill: useSyncExternalStore (React 18+) ----
function useSyncExternalStoreShim<T>(
  subscribe: (onStoreChange: () => void) => () => void,
  getSnapshot: () => T,
  _getServerSnapshot?: () => T,
): T {
  const value = getSnapshot()
  const [{ inst }, forceUpdate] = ReactOriginal.useState({
    inst: { value, getSnapshot },
  })

  ReactOriginal.useLayoutEffect(() => {
    inst.value = value
    inst.getSnapshot = getSnapshot
    if (!Object.is(inst.value, inst.getSnapshot())) {
      forceUpdate({ inst })
    }
  }, [subscribe, value, getSnapshot])

  ReactOriginal.useEffect(() => {
    if (!Object.is(inst.value, inst.getSnapshot())) {
      forceUpdate({ inst })
    }
    return subscribe(() => {
      if (!Object.is(inst.value, inst.getSnapshot())) {
        forceUpdate({ inst })
      }
    })
  }, [subscribe])

  return value
}

// ---- Polyfill: useId (React 18+) ----
let _idCounter = 0
function useIdShim(): string {
  const ref = ReactOriginal.useRef<string | null>(null)
  if (ref.current === null) {
    ref.current = `:r${_idCounter++}:`
  }
  return ref.current
}

// ---- Polyfill: useInsertionEffect (React 18+) ----
const useInsertionEffectShim = ReactOriginal.useLayoutEffect

// Build the augmented React with polyfilled hooks
const ShimmedReact = {
  ...ReactOriginal,
  useSyncExternalStore:
    ReactOriginal.useSyncExternalStore ?? useSyncExternalStoreShim,
  useId: ReactOriginal.useId ?? useIdShim,
  useInsertionEffect:
    ReactOriginal.useInsertionEffect ?? useInsertionEffectShim,
}

// React internals — react-dom accesses these via require('react')
// eslint-disable-next-line @typescript-eslint/naming-convention
export const __SECRET_INTERNALS_DO_NOT_USE_OR_YOU_WILL_BE_FIRED =
  ReactOriginal.__SECRET_INTERNALS_DO_NOT_USE_OR_YOU_WILL_BE_FIRED

// Named exports — spread all original React exports + polyfills
export const {
  Children,
  Component,
  Fragment,
  Profiler,
  PureComponent,
  StrictMode,
  Suspense,
  cloneElement,
  createContext,
  createElement,
  createFactory,
  createRef,
  forwardRef,
  isValidElement,
  lazy,
  memo,
  startTransition,
  useCallback,
  useContext,
  useDebugValue,
  useDeferredValue,
  useEffect,
  useId,
  useImperativeHandle,
  useInsertionEffect,
  useLayoutEffect,
  useMemo,
  useReducer,
  useRef,
  useState,
  useSyncExternalStore,
  useTransition,
  version,
} = ShimmedReact

export default ShimmedReact
