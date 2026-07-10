import * as z from "zod/v4-mini";
import { RiskIDRequestBody, RiskIDRequestBody$Outbound } from "../components/riskidrequestbody.js";
export type UnmaskRiskResultSecurityOption1 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type UnmaskRiskResultSecurityOption2 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type UnmaskRiskResultSecurity = {
    option1?: UnmaskRiskResultSecurityOption1 | undefined;
    option2?: UnmaskRiskResultSecurityOption2 | undefined;
};
export type UnmaskRiskResultRequest = {
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
    riskIDRequestBody: RiskIDRequestBody;
};
/** @internal */
export type UnmaskRiskResultSecurityOption1$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const UnmaskRiskResultSecurityOption1$outboundSchema: z.ZodMiniType<UnmaskRiskResultSecurityOption1$Outbound, UnmaskRiskResultSecurityOption1>;
export declare function unmaskRiskResultSecurityOption1ToJSON(unmaskRiskResultSecurityOption1: UnmaskRiskResultSecurityOption1): string;
/** @internal */
export type UnmaskRiskResultSecurityOption2$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const UnmaskRiskResultSecurityOption2$outboundSchema: z.ZodMiniType<UnmaskRiskResultSecurityOption2$Outbound, UnmaskRiskResultSecurityOption2>;
export declare function unmaskRiskResultSecurityOption2ToJSON(unmaskRiskResultSecurityOption2: UnmaskRiskResultSecurityOption2): string;
/** @internal */
export type UnmaskRiskResultSecurity$Outbound = {
    Option1?: UnmaskRiskResultSecurityOption1$Outbound | undefined;
    Option2?: UnmaskRiskResultSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const UnmaskRiskResultSecurity$outboundSchema: z.ZodMiniType<UnmaskRiskResultSecurity$Outbound, UnmaskRiskResultSecurity>;
export declare function unmaskRiskResultSecurityToJSON(unmaskRiskResultSecurity: UnmaskRiskResultSecurity): string;
/** @internal */
export type UnmaskRiskResultRequest$Outbound = {
    "Gram-Key"?: string | undefined;
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
    RiskIDRequestBody: RiskIDRequestBody$Outbound;
};
/** @internal */
export declare const UnmaskRiskResultRequest$outboundSchema: z.ZodMiniType<UnmaskRiskResultRequest$Outbound, UnmaskRiskResultRequest>;
export declare function unmaskRiskResultRequestToJSON(unmaskRiskResultRequest: UnmaskRiskResultRequest): string;
//# sourceMappingURL=unmaskriskresult.d.ts.map