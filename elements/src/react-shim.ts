/**
 * Bundler-level React shim for React 16/17. The reactCompat() Vite plugin
 * aliases 'react' to this file so named imports get polyfilled APIs.
 * NOT meant to be imported directly.
 */

// @ts-expect-error — resolved by the Vite plugin to the real react package
import * as ReactOriginal from 'react-original'
import { createShims } from './compat-shims'

const Shimmed = { ...ReactOriginal, ...createShims(ReactOriginal) }

// React internals required by react-dom
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
} = Shimmed

export default Shimmed
