// Escape hatch type — flags any-cast call sites with a required reason message
// so they're greppable and reviewable.
//
// Usage:
//   const value = apiCall() as FIXME<"Need to fix upstream API types">;
//
// Also re-expose React's JSX namespace as a global so component return types
// can be written as `JSX.Element` without importing React or referencing
// React.JSX.* explicitly. With TS 5+ and `jsx: react-jsx`, the JSX global
// is no longer ambient by default.

declare global {
  // oxlint-disable-next-line typescript/no-explicit-any
  type FIXME<M extends string> = any;

  // oxlint-disable-next-line typescript/no-namespace
  namespace JSX {
    type Element = import("react").JSX.Element;
    type ElementClass = import("react").JSX.ElementClass;
    type ElementAttributesProperty =
      import("react").JSX.ElementAttributesProperty;
    type ElementChildrenAttribute =
      import("react").JSX.ElementChildrenAttribute;
    type LibraryManagedAttributes<C, P> =
      import("react").JSX.LibraryManagedAttributes<C, P>;
    type IntrinsicAttributes = import("react").JSX.IntrinsicAttributes;
    type IntrinsicClassAttributes<T> =
      import("react").JSX.IntrinsicClassAttributes<T>;
    type IntrinsicElements = import("react").JSX.IntrinsicElements;
  }
}

export {};
