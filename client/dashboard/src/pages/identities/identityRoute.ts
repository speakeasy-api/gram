import { useOutletContext } from "react-router";
import type { IdentityModel } from "@gram/client/models/components/identitymodel.js";

/** Shape handed to every identity sub-page through the router outlet. */
export type IdentityOutletContext = {
  identity: IdentityModel;
  /** The URN exactly as it appears in the URL, already decoded. */
  urn: string;
};

export function useIdentityOutlet(): IdentityOutletContext {
  return useOutletContext<IdentityOutletContext>();
}
