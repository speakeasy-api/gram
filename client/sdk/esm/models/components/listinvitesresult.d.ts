import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { OrganizationInvitation } from "./organizationinvitation.js";
export type ListInvitesResult = {
  /**
   * Pending invitations for the organization only; accepted, expired, and revoked invitations are omitted.
   */
  invitations: Array<OrganizationInvitation>;
};
/** @internal */
export declare const ListInvitesResult$inboundSchema: z.ZodMiniType<
  ListInvitesResult,
  unknown
>;
export declare function listInvitesResultFromJSON(
  jsonString: string,
): SafeParseResult<ListInvitesResult, SDKValidationError>;
//# sourceMappingURL=listinvitesresult.d.ts.map
