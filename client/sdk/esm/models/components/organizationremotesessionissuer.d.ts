import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { RemoteSessionIssuer } from "./remotesessionissuer.js";
/**
 * An organization-administrator view of a remote_session_issuer: the issuer plus its associated client count and (for project-specific issuers) the owning project's name.
 */
export type OrganizationRemoteSessionIssuer = {
  /**
   * Number of non-deleted remote_session_clients registered with this issuer.
   */
  clientCount: number;
  /**
   * A remote_session_issuer record — upstream Authorization Server identity that Gram speaks OAuth to.
   */
  issuer: RemoteSessionIssuer;
  /**
   * The owning project's name. Empty for organizational (project_id NULL) issuers.
   */
  projectName?: string | undefined;
};
/** @internal */
export declare const OrganizationRemoteSessionIssuer$inboundSchema: z.ZodMiniType<
  OrganizationRemoteSessionIssuer,
  unknown
>;
export declare function organizationRemoteSessionIssuerFromJSON(
  jsonString: string,
): SafeParseResult<OrganizationRemoteSessionIssuer, SDKValidationError>;
//# sourceMappingURL=organizationremotesessionissuer.d.ts.map
