import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { OrganizationRemoteSessionClient } from "./organizationremotesessionclient.js";
/**
 * Result type for the organization-administrator client listing for a single issuer.
 */
export type ListOrganizationRemoteSessionClientsResult = {
    items: Array<OrganizationRemoteSessionClient>;
    /**
     * Cursor for the next page; empty when exhausted.
     */
    nextCursor?: string | undefined;
};
/** @internal */
export declare const ListOrganizationRemoteSessionClientsResult$inboundSchema: z.ZodMiniType<ListOrganizationRemoteSessionClientsResult, unknown>;
export declare function listOrganizationRemoteSessionClientsResultFromJSON(jsonString: string): SafeParseResult<ListOrganizationRemoteSessionClientsResult, SDKValidationError>;
//# sourceMappingURL=listorganizationremotesessionclientsresult.d.ts.map