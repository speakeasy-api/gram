import {
  createContext,
  type ReactElement,
  type ReactNode,
  useContext,
} from "react";

import type { LinkResolver, MarkdownLinkComponent } from "@/types";

/**
 * Host-supplied hooks for rendering links inside assistant markdown.
 *
 * - `resolveLink` decides *where* a link points (e.g. rewriting an inline
 *   entity reference to a real route into the host app).
 * - `LinkComponent` decides *how* a link renders (e.g. the host's design-system
 *   link). Elements falls back to a plain `<a>` when none is supplied, so
 *   standalone usage keeps working.
 */
export interface MarkdownLinkValue {
  resolveLink?: LinkResolver;
  LinkComponent?: MarkdownLinkComponent;
}

const MarkdownLinkContext = createContext<MarkdownLinkValue>({});

export const MarkdownLinkProvider = ({
  value,
  children,
}: {
  value: MarkdownLinkValue;
  children: ReactNode;
}): ReactElement => (
  <MarkdownLinkContext.Provider value={value}>
    {children}
  </MarkdownLinkContext.Provider>
);

/** Read the host-supplied link hooks, or an empty object if none. */
export const useMarkdownLink = (): MarkdownLinkValue =>
  useContext(MarkdownLinkContext);
