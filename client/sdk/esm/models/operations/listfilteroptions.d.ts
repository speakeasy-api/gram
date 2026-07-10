import * as z from "zod/v4-mini";
import { ListFilterOptionsPayload, ListFilterOptionsPayload$Outbound } from "../components/listfilteroptionspayload.js";
export type ListFilterOptionsSecurityOption1 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type ListFilterOptionsSecurityOption2 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type ListFilterOptionsSecurity = {
    option1?: ListFilterOptionsSecurityOption1 | undefined;
    option2?: ListFilterOptionsSecurityOption2 | undefined;
};
export type ListFilterOptionsRequest = {
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
    listFilterOptionsPayload: ListFilterOptionsPayload;
};
/** @internal */
export type ListFilterOptionsSecurityOption1$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const ListFilterOptionsSecurityOption1$outboundSchema: z.ZodMiniType<ListFilterOptionsSecurityOption1$Outbound, ListFilterOptionsSecurityOption1>;
export declare function listFilterOptionsSecurityOption1ToJSON(listFilterOptionsSecurityOption1: ListFilterOptionsSecurityOption1): string;
/** @internal */
export type ListFilterOptionsSecurityOption2$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const ListFilterOptionsSecurityOption2$outboundSchema: z.ZodMiniType<ListFilterOptionsSecurityOption2$Outbound, ListFilterOptionsSecurityOption2>;
export declare function listFilterOptionsSecurityOption2ToJSON(listFilterOptionsSecurityOption2: ListFilterOptionsSecurityOption2): string;
/** @internal */
export type ListFilterOptionsSecurity$Outbound = {
    Option1?: ListFilterOptionsSecurityOption1$Outbound | undefined;
    Option2?: ListFilterOptionsSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const ListFilterOptionsSecurity$outboundSchema: z.ZodMiniType<ListFilterOptionsSecurity$Outbound, ListFilterOptionsSecurity>;
export declare function listFilterOptionsSecurityToJSON(listFilterOptionsSecurity: ListFilterOptionsSecurity): string;
/** @internal */
export type ListFilterOptionsRequest$Outbound = {
    "Gram-Key"?: string | undefined;
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
    ListFilterOptionsPayload: ListFilterOptionsPayload$Outbound;
};
/** @internal */
export declare const ListFilterOptionsRequest$outboundSchema: z.ZodMiniType<ListFilterOptionsRequest$Outbound, ListFilterOptionsRequest>;
export declare function listFilterOptionsRequestToJSON(listFilterOptionsRequest: ListFilterOptionsRequest): string;
//# sourceMappingURL=listfilteroptions.d.ts.map