import * as z from "zod/v4-mini";
import { ListToolUsageTracesPayload, ListToolUsageTracesPayload$Outbound } from "../components/listtoolusagetracespayload.js";
export type ListToolUsageTracesSecurityOption1 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type ListToolUsageTracesSecurityOption2 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type ListToolUsageTracesSecurity = {
    option1?: ListToolUsageTracesSecurityOption1 | undefined;
    option2?: ListToolUsageTracesSecurityOption2 | undefined;
};
export type ListToolUsageTracesRequest = {
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
    listToolUsageTracesPayload: ListToolUsageTracesPayload;
};
/** @internal */
export type ListToolUsageTracesSecurityOption1$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const ListToolUsageTracesSecurityOption1$outboundSchema: z.ZodMiniType<ListToolUsageTracesSecurityOption1$Outbound, ListToolUsageTracesSecurityOption1>;
export declare function listToolUsageTracesSecurityOption1ToJSON(listToolUsageTracesSecurityOption1: ListToolUsageTracesSecurityOption1): string;
/** @internal */
export type ListToolUsageTracesSecurityOption2$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const ListToolUsageTracesSecurityOption2$outboundSchema: z.ZodMiniType<ListToolUsageTracesSecurityOption2$Outbound, ListToolUsageTracesSecurityOption2>;
export declare function listToolUsageTracesSecurityOption2ToJSON(listToolUsageTracesSecurityOption2: ListToolUsageTracesSecurityOption2): string;
/** @internal */
export type ListToolUsageTracesSecurity$Outbound = {
    Option1?: ListToolUsageTracesSecurityOption1$Outbound | undefined;
    Option2?: ListToolUsageTracesSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const ListToolUsageTracesSecurity$outboundSchema: z.ZodMiniType<ListToolUsageTracesSecurity$Outbound, ListToolUsageTracesSecurity>;
export declare function listToolUsageTracesSecurityToJSON(listToolUsageTracesSecurity: ListToolUsageTracesSecurity): string;
/** @internal */
export type ListToolUsageTracesRequest$Outbound = {
    "Gram-Key"?: string | undefined;
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
    ListToolUsageTracesPayload: ListToolUsageTracesPayload$Outbound;
};
/** @internal */
export declare const ListToolUsageTracesRequest$outboundSchema: z.ZodMiniType<ListToolUsageTracesRequest$Outbound, ListToolUsageTracesRequest>;
export declare function listToolUsageTracesRequestToJSON(listToolUsageTracesRequest: ListToolUsageTracesRequest): string;
//# sourceMappingURL=listtoolusagetraces.d.ts.map