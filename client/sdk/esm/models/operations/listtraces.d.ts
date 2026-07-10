import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
export type ListTracesSecurityOption1 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type ListTracesSecurityOption2 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type ListTracesSecurityOption3 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type ListTracesSecurityOption4 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type ListTracesSecurity = {
    option1?: ListTracesSecurityOption1 | undefined;
    option2?: ListTracesSecurityOption2 | undefined;
    option3?: ListTracesSecurityOption3 | undefined;
    option4?: ListTracesSecurityOption4 | undefined;
};
/**
 * Sort order
 */
export declare const ListTracesQueryParamSort: {
    readonly Asc: "asc";
    readonly Desc: "desc";
};
/**
 * Sort order
 */
export type ListTracesQueryParamSort = ClosedEnum<typeof ListTracesQueryParamSort>;
export type ListTracesRequest = {
    /**
     * Start time in Unix nanoseconds
     */
    timeStart?: number | undefined;
    /**
     * End time in Unix nanoseconds
     */
    timeEnd?: number | undefined;
    /**
     * Deployment ID filter
     */
    deploymentId?: string | undefined;
    /**
     * Function ID filter
     */
    functionId?: string | undefined;
    /**
     * Cursor for pagination (trace ID)
     */
    cursor?: string | undefined;
    /**
     * Number of items to return (1-1000)
     */
    limit?: number | undefined;
    /**
     * Sort order
     */
    sort?: ListTracesQueryParamSort | undefined;
    /**
     * API Key header
     */
    gramKey?: string | undefined;
    /**
     * Session header
     */
    gramSession?: string | undefined;
    /**
     * project header
     */
    gramProject?: string | undefined;
};
/** @internal */
export type ListTracesSecurityOption1$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const ListTracesSecurityOption1$outboundSchema: z.ZodMiniType<ListTracesSecurityOption1$Outbound, ListTracesSecurityOption1>;
export declare function listTracesSecurityOption1ToJSON(listTracesSecurityOption1: ListTracesSecurityOption1): string;
/** @internal */
export type ListTracesSecurityOption2$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const ListTracesSecurityOption2$outboundSchema: z.ZodMiniType<ListTracesSecurityOption2$Outbound, ListTracesSecurityOption2>;
export declare function listTracesSecurityOption2ToJSON(listTracesSecurityOption2: ListTracesSecurityOption2): string;
/** @internal */
export type ListTracesSecurityOption3$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const ListTracesSecurityOption3$outboundSchema: z.ZodMiniType<ListTracesSecurityOption3$Outbound, ListTracesSecurityOption3>;
export declare function listTracesSecurityOption3ToJSON(listTracesSecurityOption3: ListTracesSecurityOption3): string;
/** @internal */
export type ListTracesSecurityOption4$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const ListTracesSecurityOption4$outboundSchema: z.ZodMiniType<ListTracesSecurityOption4$Outbound, ListTracesSecurityOption4>;
export declare function listTracesSecurityOption4ToJSON(listTracesSecurityOption4: ListTracesSecurityOption4): string;
/** @internal */
export type ListTracesSecurity$Outbound = {
    Option1?: ListTracesSecurityOption1$Outbound | undefined;
    Option2?: ListTracesSecurityOption2$Outbound | undefined;
    Option3?: ListTracesSecurityOption3$Outbound | undefined;
    Option4?: ListTracesSecurityOption4$Outbound | undefined;
};
/** @internal */
export declare const ListTracesSecurity$outboundSchema: z.ZodMiniType<ListTracesSecurity$Outbound, ListTracesSecurity>;
export declare function listTracesSecurityToJSON(listTracesSecurity: ListTracesSecurity): string;
/** @internal */
export declare const ListTracesQueryParamSort$outboundSchema: z.ZodMiniEnum<typeof ListTracesQueryParamSort>;
/** @internal */
export type ListTracesRequest$Outbound = {
    time_start?: number | undefined;
    time_end?: number | undefined;
    deployment_id?: string | undefined;
    function_id?: string | undefined;
    cursor?: string | undefined;
    limit: number;
    sort: string;
    "Gram-Key"?: string | undefined;
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const ListTracesRequest$outboundSchema: z.ZodMiniType<ListTracesRequest$Outbound, ListTracesRequest>;
export declare function listTracesRequestToJSON(listTracesRequest: ListTracesRequest): string;
//# sourceMappingURL=listtraces.d.ts.map