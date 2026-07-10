import * as z from "zod/v4-mini";
import { UpsertGlobalToolVariationForm, UpsertGlobalToolVariationForm$Outbound } from "../components/upsertglobaltoolvariationform.js";
export type UpsertGlobalVariationSecurityOption1 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type UpsertGlobalVariationSecurityOption2 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type UpsertGlobalVariationSecurity = {
    option1?: UpsertGlobalVariationSecurityOption1 | undefined;
    option2?: UpsertGlobalVariationSecurityOption2 | undefined;
};
export type UpsertGlobalVariationRequest = {
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
    upsertGlobalToolVariationForm: UpsertGlobalToolVariationForm;
};
/** @internal */
export type UpsertGlobalVariationSecurityOption1$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const UpsertGlobalVariationSecurityOption1$outboundSchema: z.ZodMiniType<UpsertGlobalVariationSecurityOption1$Outbound, UpsertGlobalVariationSecurityOption1>;
export declare function upsertGlobalVariationSecurityOption1ToJSON(upsertGlobalVariationSecurityOption1: UpsertGlobalVariationSecurityOption1): string;
/** @internal */
export type UpsertGlobalVariationSecurityOption2$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const UpsertGlobalVariationSecurityOption2$outboundSchema: z.ZodMiniType<UpsertGlobalVariationSecurityOption2$Outbound, UpsertGlobalVariationSecurityOption2>;
export declare function upsertGlobalVariationSecurityOption2ToJSON(upsertGlobalVariationSecurityOption2: UpsertGlobalVariationSecurityOption2): string;
/** @internal */
export type UpsertGlobalVariationSecurity$Outbound = {
    Option1?: UpsertGlobalVariationSecurityOption1$Outbound | undefined;
    Option2?: UpsertGlobalVariationSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const UpsertGlobalVariationSecurity$outboundSchema: z.ZodMiniType<UpsertGlobalVariationSecurity$Outbound, UpsertGlobalVariationSecurity>;
export declare function upsertGlobalVariationSecurityToJSON(upsertGlobalVariationSecurity: UpsertGlobalVariationSecurity): string;
/** @internal */
export type UpsertGlobalVariationRequest$Outbound = {
    "Gram-Session"?: string | undefined;
    "Gram-Key"?: string | undefined;
    "Gram-Project"?: string | undefined;
    UpsertGlobalToolVariationForm: UpsertGlobalToolVariationForm$Outbound;
};
/** @internal */
export declare const UpsertGlobalVariationRequest$outboundSchema: z.ZodMiniType<UpsertGlobalVariationRequest$Outbound, UpsertGlobalVariationRequest>;
export declare function upsertGlobalVariationRequestToJSON(upsertGlobalVariationRequest: UpsertGlobalVariationRequest): string;
//# sourceMappingURL=upsertglobalvariation.d.ts.map