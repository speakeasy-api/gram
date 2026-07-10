import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { ListOrganizationRemoteSessionClientsResult } from "../components/listorganizationremotesessionclientsresult.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type ListOrganizationRemoteSessionClientsSecurity = {
    sessionHeaderGramSession?: string | undefined;
    apikeyHeaderGramKey?: string | undefined;
};
export type ListOrganizationRemoteSessionClientsRequest = {
    /**
     * The remote_session_issuer id to list clients for.
     */
    issuerId: string;
    /**
     * Pagination cursor.
     */
    cursor?: string | undefined;
    /**
     * Page size (default 50, max 100).
     */
    limit?: number | undefined;
    /**
     * Session header
     */
    gramSession?: string | undefined;
    /**
     * API Key header
     */
    gramKey?: string | undefined;
};
export type ListOrganizationRemoteSessionClientsResponse = {
    result: ListOrganizationRemoteSessionClientsResult;
};
/** @internal */
export type ListOrganizationRemoteSessionClientsSecurity$Outbound = {
    "session_header_Gram-Session"?: string | undefined;
    "apikey_header_Gram-Key"?: string | undefined;
};
/** @internal */
export declare const ListOrganizationRemoteSessionClientsSecurity$outboundSchema: z.ZodMiniType<ListOrganizationRemoteSessionClientsSecurity$Outbound, ListOrganizationRemoteSessionClientsSecurity>;
export declare function listOrganizationRemoteSessionClientsSecurityToJSON(listOrganizationRemoteSessionClientsSecurity: ListOrganizationRemoteSessionClientsSecurity): string;
/** @internal */
export type ListOrganizationRemoteSessionClientsRequest$Outbound = {
    issuer_id: string;
    cursor?: string | undefined;
    limit?: number | undefined;
    "Gram-Session"?: string | undefined;
    "Gram-Key"?: string | undefined;
};
/** @internal */
export declare const ListOrganizationRemoteSessionClientsRequest$outboundSchema: z.ZodMiniType<ListOrganizationRemoteSessionClientsRequest$Outbound, ListOrganizationRemoteSessionClientsRequest>;
export declare function listOrganizationRemoteSessionClientsRequestToJSON(listOrganizationRemoteSessionClientsRequest: ListOrganizationRemoteSessionClientsRequest): string;
/** @internal */
export declare const ListOrganizationRemoteSessionClientsResponse$inboundSchema: z.ZodMiniType<ListOrganizationRemoteSessionClientsResponse, unknown>;
export declare function listOrganizationRemoteSessionClientsResponseFromJSON(jsonString: string): SafeParseResult<ListOrganizationRemoteSessionClientsResponse, SDKValidationError>;
//# sourceMappingURL=listorganizationremotesessionclients.d.ts.map