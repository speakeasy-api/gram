import * as z from "zod/v4-mini";
import { RiskIDRequestBody, RiskIDRequestBody$Outbound } from "../components/riskidrequestbody.js";
export type DenyRiskPolicyBypassRequestSecurityOption1 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type DenyRiskPolicyBypassRequestSecurityOption2 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type DenyRiskPolicyBypassRequestSecurity = {
    option1?: DenyRiskPolicyBypassRequestSecurityOption1 | undefined;
    option2?: DenyRiskPolicyBypassRequestSecurityOption2 | undefined;
};
export type DenyRiskPolicyBypassRequestRequest = {
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
export type DenyRiskPolicyBypassRequestSecurityOption1$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const DenyRiskPolicyBypassRequestSecurityOption1$outboundSchema: z.ZodMiniType<DenyRiskPolicyBypassRequestSecurityOption1$Outbound, DenyRiskPolicyBypassRequestSecurityOption1>;
export declare function denyRiskPolicyBypassRequestSecurityOption1ToJSON(denyRiskPolicyBypassRequestSecurityOption1: DenyRiskPolicyBypassRequestSecurityOption1): string;
/** @internal */
export type DenyRiskPolicyBypassRequestSecurityOption2$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const DenyRiskPolicyBypassRequestSecurityOption2$outboundSchema: z.ZodMiniType<DenyRiskPolicyBypassRequestSecurityOption2$Outbound, DenyRiskPolicyBypassRequestSecurityOption2>;
export declare function denyRiskPolicyBypassRequestSecurityOption2ToJSON(denyRiskPolicyBypassRequestSecurityOption2: DenyRiskPolicyBypassRequestSecurityOption2): string;
/** @internal */
export type DenyRiskPolicyBypassRequestSecurity$Outbound = {
    Option1?: DenyRiskPolicyBypassRequestSecurityOption1$Outbound | undefined;
    Option2?: DenyRiskPolicyBypassRequestSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const DenyRiskPolicyBypassRequestSecurity$outboundSchema: z.ZodMiniType<DenyRiskPolicyBypassRequestSecurity$Outbound, DenyRiskPolicyBypassRequestSecurity>;
export declare function denyRiskPolicyBypassRequestSecurityToJSON(denyRiskPolicyBypassRequestSecurity: DenyRiskPolicyBypassRequestSecurity): string;
/** @internal */
export type DenyRiskPolicyBypassRequestRequest$Outbound = {
    "Gram-Key"?: string | undefined;
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
    RiskIDRequestBody: RiskIDRequestBody$Outbound;
};
/** @internal */
export declare const DenyRiskPolicyBypassRequestRequest$outboundSchema: z.ZodMiniType<DenyRiskPolicyBypassRequestRequest$Outbound, DenyRiskPolicyBypassRequestRequest>;
export declare function denyRiskPolicyBypassRequestRequestToJSON(denyRiskPolicyBypassRequestRequest: DenyRiskPolicyBypassRequestRequest): string;
//# sourceMappingURL=denyriskpolicybypassrequest.d.ts.map