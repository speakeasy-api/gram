import { jsx as _jsx } from "react/jsx-runtime";
import React from "react";
const GramContext = React.createContext(null);
export function GramProvider(props) {
  return _jsx(GramContext.Provider, {
    value: props.client,
    children: props.children,
  });
}
export function useGramContext() {
  const value = React.useContext(GramContext);
  if (value === null) {
    throw new Error(
      "SDK not initialized. Create an instance of GramCore and pass it to <GramProvider />.",
    );
  }
  return value;
}
//# sourceMappingURL=_context.js.map
