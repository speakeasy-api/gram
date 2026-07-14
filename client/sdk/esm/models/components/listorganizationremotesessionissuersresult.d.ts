import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { OrganizationRemoteSessionIssuer } from "./organizationremotesessionissuer.js";
/**
 * Result type for the organization-administrator issuer listing — organizational and project-specific issuers across the org.
 */
export type ListOrganizationRemoteSessionIssuersResult = {
  items: Array<OrganizationRemoteSessionIssuer>;
  /**
   * Cursor for the next page; empty when exhausted.
   */
  nextCursor?: string | undefined;
};
/** @internal */
export declare const ListOrganizationRemoteSessionIssuersResult$inboundSchema: z.ZodMiniType<
  ListOrganizationRemoteSessionIssuersResult,
  unknown
>;
export declare function listOrganizationRemoteSessionIssuersResultFromJSON(
  jsonString: string,
): SafeParseResult<
  ListOrganizationRemoteSessionIssuersResult,
  SDKValidationError
>;
//# sourceMappingURL=listorganizationremotesessionissuersresult.d.ts.map
