import * as z from "zod/v4-mini";
export type ListTriggerInstancesSecurity = {
    projectSlugHeaderGramProject?: string | undefined;
    sessionHeaderGramSession?: string | undefined;
};
export type ListTriggerInstancesRequest = {
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
export type ListTriggerInstancesSecurity$Outbound = {
    "project_slug_header_Gram-Project"?: string | undefined;
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const ListTriggerInstancesSecurity$outboundSchema: z.ZodMiniType<ListTriggerInstancesSecurity$Outbound, ListTriggerInstancesSecurity>;
export declare function listTriggerInstancesSecurityToJSON(listTriggerInstancesSecurity: ListTriggerInstancesSecurity): string;
/** @internal */
export type ListTriggerInstancesRequest$Outbound = {
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const ListTriggerInstancesRequest$outboundSchema: z.ZodMiniType<ListTriggerInstancesRequest$Outbound, ListTriggerInstancesRequest>;
export declare function listTriggerInstancesRequestToJSON(listTriggerInstancesRequest: ListTriggerInstancesRequest): string;
//# sourceMappingURL=listtriggerinstances.d.ts.map