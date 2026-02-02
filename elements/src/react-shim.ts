/**
 * React shim for older versions (16.8+, 17).
 *
 * Named imports like `import { useId } from 'react'` bind at module load
 * time, so runtime patching (compat.ts) alone cannot inject missing APIs.
 * This module re-exports everything from the real React package with
 * polyfills for APIs introduced in React 18.
 *
 * This file is NOT imported directly — the reactCompat() Vite plugin
 * redirects `react` imports here via resolve.alias.
 */

// @ts-expect-error — 'react-original' is resolved by the Vite plugin to the real react package
import * as ReactOriginal from 'react-original'

import { createShims } from './compat-shims'

const shims = createShims(ReactOriginal)

const ShimmedReact = {
  ...ReactOriginal,
  ...shims,
}

// React internals — react-dom accesses these via require('react')
export const __SECRET_INTERNALS_DO_NOT_USE_OR_YOU_WILL_BE_FIRED =
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  (ReactOriginal as any).__SECRET_INTERNALS_DO_NOT_USE_OR_YOU_WILL_BE_FIRED

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
