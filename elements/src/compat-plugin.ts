/**
 * Vite plugin for React 16/17 compatibility.
 *
 * Intercepts `import ... from 'react'` and redirects through a virtual shim
 * module that polyfills React 18 APIs (useSyncExternalStore, useId,
 * useInsertionEffect). This is necessary because named imports bind at module
 * load time — runtime patching (compat.ts) alone cannot inject missing APIs
 * into third-party libraries like zustand or @assistant-ui/react.
 *
 * Usage:
 * ```ts
 * import { reactCompat } from '@gram-ai/elements/compat'
 *
 * export default defineConfig({
 *   plugins: [react(), reactCompat()],
 * })
 * ```
 */

import { createRequire } from 'node:module'
import { resolve } from 'node:path'
import type { Plugin } from 'vite'

const VIRTUAL_SHIM_ID = '\0gram-react-compat-shim'

/**
 * The virtual shim module source. Imports the real React via
 * 'react-original' (resolved by the plugin to the actual react package)
 * and re-exports everything with polyfilled hooks.
 */
const SHIM_SOURCE = /* js */ `
import ReactOriginal from 'react-original';

// ---- Polyfill: useSyncExternalStore (React 18+) ----
function useSyncExternalStoreShim(subscribe, getSnapshot) {
  var value = getSnapshot();
  var ref = ReactOriginal.useState({ inst: { value: value, getSnapshot: getSnapshot } });
  var inst = ref[0].inst;
  var forceUpdate = ref[1];

  ReactOriginal.useLayoutEffect(function () {
    inst.value = value;
    inst.getSnapshot = getSnapshot;
    if (!Object.is(inst.value, inst.getSnapshot())) forceUpdate({ inst: inst });
  }, [subscribe, value, getSnapshot]);

  ReactOriginal.useEffect(function () {
    if (!Object.is(inst.value, inst.getSnapshot())) forceUpdate({ inst: inst });
    return subscribe(function () {
      if (!Object.is(inst.value, inst.getSnapshot())) forceUpdate({ inst: inst });
    });
  }, [subscribe]);

  return value;
}

// ---- Polyfill: useId (React 18+) ----
var _idCounter = 0;
function useIdShim() {
  var ref = ReactOriginal.useRef(null);
  if (ref.current === null) ref.current = ':r' + _idCounter++ + ':';
  return ref.current;
}

var ShimmedReact = Object.assign({}, ReactOriginal, {
  useSyncExternalStore: ReactOriginal.useSyncExternalStore || useSyncExternalStoreShim,
  useId: ReactOriginal.useId || useIdShim,
  useInsertionEffect: ReactOriginal.useInsertionEffect || ReactOriginal.useLayoutEffect,
});

// React internals — react-dom accesses these via require('react')
export var __SECRET_INTERNALS_DO_NOT_USE_OR_YOU_WILL_BE_FIRED =
  ReactOriginal.__SECRET_INTERNALS_DO_NOT_USE_OR_YOU_WILL_BE_FIRED;

export var Children = ShimmedReact.Children;
export var Component = ShimmedReact.Component;
export var Fragment = ShimmedReact.Fragment;
export var Profiler = ShimmedReact.Profiler;
export var PureComponent = ShimmedReact.PureComponent;
export var StrictMode = ShimmedReact.StrictMode;
export var Suspense = ShimmedReact.Suspense;
export var cloneElement = ShimmedReact.cloneElement;
export var createContext = ShimmedReact.createContext;
export var createElement = ShimmedReact.createElement;
export var createFactory = ShimmedReact.createFactory;
export var createRef = ShimmedReact.createRef;
export var forwardRef = ShimmedReact.forwardRef;
export var isValidElement = ShimmedReact.isValidElement;
export var lazy = ShimmedReact.lazy;
export var memo = ShimmedReact.memo;
export var startTransition = ShimmedReact.startTransition;
export var useCallback = ShimmedReact.useCallback;
export var useContext = ShimmedReact.useContext;
export var useDebugValue = ShimmedReact.useDebugValue;
export var useDeferredValue = ShimmedReact.useDeferredValue;
export var useEffect = ShimmedReact.useEffect;
export var useId = ShimmedReact.useId;
export var useImperativeHandle = ShimmedReact.useImperativeHandle;
export var useInsertionEffect = ShimmedReact.useInsertionEffect;
export var useLayoutEffect = ShimmedReact.useLayoutEffect;
export var useMemo = ShimmedReact.useMemo;
export var useReducer = ShimmedReact.useReducer;
export var useRef = ShimmedReact.useRef;
export var useState = ShimmedReact.useState;
export var useSyncExternalStore = ShimmedReact.useSyncExternalStore;
export var useTransition = ShimmedReact.useTransition;
export var version = ShimmedReact.version;
export default ShimmedReact;
`

/**
 * Vite plugin that shims React 18 APIs for React 16/17 projects.
 *
 * Redirects all `import ... from 'react'` statements (including from
 * dependencies) through a virtual module that polyfills missing hooks.
 * Safe to use with React 18/19 — existing APIs take precedence via `||`.
 */
export function reactCompat(): Plugin {
  let realReactEntry: string

  return {
    name: 'gram-elements-react-compat',
    enforce: 'pre',

    config() {
      return {
        resolve: {
          dedupe: ['react', 'react-dom'],
        },
      }
    },

    configResolved(config) {
      const require = createRequire(resolve(config.root, 'package.json'))
      realReactEntry = require.resolve('react')
    },

    resolveId(id) {
      if (id === 'react') return VIRTUAL_SHIM_ID
      if (id === 'react-original') return realReactEntry
      return null
    },

    load(id) {
      if (id === VIRTUAL_SHIM_ID) return SHIM_SOURCE
      return null
    },
  }
}
