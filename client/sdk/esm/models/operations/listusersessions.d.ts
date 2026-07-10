import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { ListUserSessionsResult } from "../components/listusersessionsresult.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type ListUserSessionsSecurityOption1 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type ListUserSessionsSecurityOption2 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type ListUserSessionsSecurity = {
    option1?: ListUserSessionsSecurityOption1 | undefined;
    option2?: ListUserSessionsSecurityOption2 | undefined;
};
/**
 * Filter by session status.
 */
export declare const ListUserSessionsQueryParamStatus: {
    readonly Active: "active";
    readonly Expired: "expired";
    readonly Revoked: "revoked";
    readonly All: "all";
};
/**
 * Filter by session status.
 */
export type ListUserSessionsQueryParamStatus = ClosedEnum<typeof ListUserSessionsQueryParamStatus>;
export type ListUserSessionsRequest = {
    /**
     * Exact-match filter on subject URN.
     */
    subjectUrn?: string | undefined;
    /**
     * Filter by user_session_issuer id.
     */
    userSessionIssuerId?: string | undefined;
    /**
     * Filter by session status.
     */
    status?: ListUserSessionsQueryParamStatus | undefined;
    /**
     * Filter by the connecting client id.
     */
    clientId?: string | undefined;
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
export type ListUserSessionsResponse = {
    result: ListUserSessionsResult;
};
/** @internal */
export type ListUserSessionsSecurityOption1$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const ListUserSessionsSecurityOption1$outboundSchema: z.ZodMiniType<ListUserSessionsSecurityOption1$Outbound, ListUserSessionsSecurityOption1>;
export declare function listUserSessionsSecurityOption1ToJSON(listUserSessionsSecurityOption1: ListUserSessionsSecurityOption1): string;
/** @internal */
export type ListUserSessionsSecurityOption2$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const ListUserSessionsSecurityOption2$outboundSchema: z.ZodMiniType<ListUserSessionsSecurityOption2$Outbound, ListUserSessionsSecurityOption2>;
export declare function listUserSessionsSecurityOption2ToJSON(listUserSessionsSecurityOption2: ListUserSessionsSecurityOption2): string;
/** @internal */
export type ListUserSessionsSecurity$Outbound = {
    Option1?: ListUserSessionsSecurityOption1$Outbound | undefined;
    Option2?: ListUserSessionsSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const ListUserSessionsSecurity$outboundSchema: z.ZodMiniType<ListUserSessionsSecurity$Outbound, ListUserSessionsSecurity>;
export declare function listUserSessionsSecurityToJSON(listUserSessionsSecurity: ListUserSessionsSecurity): string;
/** @internal */
export declare const ListUserSessionsQueryParamStatus$outboundSchema: z.ZodMiniEnum<typeof ListUserSessionsQueryParamStatus>;
/** @internal */
export type ListUserSessionsRequest$Outbound = {
    subject_urn?: string | undefined;
    user_session_issuer_id?: string | undefined;
    status?: string | undefined;
    client_id?: string | undefined;
    cursor?: string | undefined;
    limit?: number | undefined;
    "Gram-Session"?: string | undefined;
    "Gram-Key"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const ListUserSessionsRequest$outboundSchema: z.ZodMiniType<ListUserSessionsRequest$Outbound, ListUserSessionsRequest>;
export declare function listUserSessionsRequestToJSON(listUserSessionsRequest: ListUserSessionsRequest): string;
/** @internal */
export declare const ListUserSessionsResponse$inboundSchema: z.ZodMiniType<ListUserSessionsResponse, unknown>;
export declare function listUserSessionsResponseFromJSON(jsonString: string): SafeParseResult<ListUserSessionsResponse, SDKValidationError>;
//# sourceMappingURL=listusersessions.d.ts.map