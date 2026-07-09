
import React from "react";

import { GramCore } from "../core.js";

const GramContext = React.createContext<GramCore | null>(null);

export function GramProvider(props: { client: GramCore, children: React.ReactNode }): React.ReactNode { 
  return (
    <GramContext.Provider value={props.client}>
      {props.children}
    </GramContext.Provider>
  );
}

export function useGramContext(): GramCore { 
  const value = React.useContext(GramContext);
  if (value === null) {
    throw new Error("SDK not initialized. Create an instance of GramCore and pass it to <GramProvider />.");
  }
  return value;
}
