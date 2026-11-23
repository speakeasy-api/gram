import type { Mark } from "../kit/types";

/** How people sign in to a catalog server once it is added. */
export type CatalogSignIn = "automatic" | "manual" | "none" | "unknown";

export interface CatalogRow {
  /** The catalog reference, e.g. `com.linear/mcp`. */
  id: string;
  name: string;
  description: string;
  signIn: CatalogSignIn;
  toolCount?: number;
  /** This project already has a server from this entry. */
  added: boolean;
  mark: Mark;
}

export interface CatalogRecord extends CatalogRow {
  version: string;
  url?: string;
  repository?: string;
  readOnly: boolean;
}

export const SIGN_IN_LABEL: Record<CatalogSignIn, string> = {
  automatic: "Sign-in works out of the box",
  manual: "Needs an app registration",
  none: "No sign-in",
  unknown: "Check the setup guide",
};
