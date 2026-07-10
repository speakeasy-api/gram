import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { ListUserSessionClientsResult } from "../components/listusersessionclientsresult.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type ListUserSessionClientsSecurityOption1 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type ListUserSessionClientsSecurityOption2 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type ListUserSessionClientsSecurity = {
    option1?: ListUserSessionClientsSecurityOption1 | undefined;
    option2?: ListUserSessionClientsSecurityOption2 | undefined;
};
export type ListUserSessionClientsRequest = {
    /**
     * Filter to clients registered with this issuer.
     */
    userSessionIssuerId?: string | undefined;
    /**
     * Pagination cursor: id of the last item from the previous page.
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
    /**
     * project header
     */
    gramProject?: string | undefined;
};
export type ListUserSessionClientsResponse = {
    result: ListUserSessionClientsResult;
};
/** @internal */
export type ListUserSessionClientsSecurityOption1$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const ListUserSessionClientsSecurityOption1$outboundSchema: z.ZodMiniType<ListUserSessionClientsSecurityOption1$Outbound, ListUserSessionClientsSecurityOption1>;
export declare function listUserSessionClientsSecurityOption1ToJSON(listUserSessionClientsSecurityOption1: ListUserSessionClientsSecurityOption1): string;
/** @internal */
export type ListUserSessionClientsSecurityOption2$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const ListUserSessionClientsSecurityOption2$outboundSchema: z.ZodMiniType<ListUserSessionClientsSecurityOption2$Outbound, ListUserSessionClientsSecurityOption2>;
export declare function listUserSessionClientsSecurityOption2ToJSON(listUserSessionClientsSecurityOption2: ListUserSessionClientsSecurityOption2): string;
/** @internal */
export type ListUserSessionClientsSecurity$Outbound = {
    Option1?: ListUserSessionClientsSecurityOption1$Outbound | undefined;
    Option2?: ListUserSessionClientsSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const ListUserSessionClientsSecurity$outboundSchema: z.ZodMiniType<ListUserSessionClientsSecurity$Outbound, ListUserSessionClientsSecurity>;
export declare function listUserSessionClientsSecurityToJSON(listUserSessionClientsSecurity: ListUserSessionClientsSecurity): string;
/** @internal */
export type ListUserSessionClientsRequest$Outbound = {
    user_session_issuer_id?: string | undefined;
    cursor?: string | undefined;
    limit?: number | undefined;
    "Gram-Session"?: string | undefined;
    "Gram-Key"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const ListUserSessionClientsRequest$outboundSchema: z.ZodMiniType<ListUserSessionClientsRequest$Outbound, ListUserSessionClientsRequest>;
export declare function listUserSessionClientsRequestToJSON(listUserSessionClientsRequest: ListUserSessionClientsRequest): string;
/** @internal */
export declare const ListUserSessionClientsResponse$inboundSchema: z.ZodMiniType<ListUserSessionClientsResponse, unknown>;
export declare function listUserSessionClientsResponseFromJSON(jsonString: string): SafeParseResult<ListUserSessionClientsResponse, SDKValidationError>;
//# sourceMappingURL=listusersessionclients.d.ts.map