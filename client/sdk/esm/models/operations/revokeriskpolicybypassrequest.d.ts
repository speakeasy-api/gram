import * as z from "zod/v4-mini";
import { RiskIDRequestBody, RiskIDRequestBody$Outbound } from "../components/riskidrequestbody.js";
export type RevokeRiskPolicyBypassRequestSecurityOption1 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type RevokeRiskPolicyBypassRequestSecurityOption2 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type RevokeRiskPolicyBypassRequestSecurity = {
    option1?: RevokeRiskPolicyBypassRequestSecurityOption1 | undefined;
    option2?: RevokeRiskPolicyBypassRequestSecurityOption2 | undefined;
};
export type RevokeRiskPolicyBypassRequestRequest = {
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
export type RevokeRiskPolicyBypassRequestSecurityOption1$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const RevokeRiskPolicyBypassRequestSecurityOption1$outboundSchema: z.ZodMiniType<RevokeRiskPolicyBypassRequestSecurityOption1$Outbound, RevokeRiskPolicyBypassRequestSecurityOption1>;
export declare function revokeRiskPolicyBypassRequestSecurityOption1ToJSON(revokeRiskPolicyBypassRequestSecurityOption1: RevokeRiskPolicyBypassRequestSecurityOption1): string;
/** @internal */
export type RevokeRiskPolicyBypassRequestSecurityOption2$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const RevokeRiskPolicyBypassRequestSecurityOption2$outboundSchema: z.ZodMiniType<RevokeRiskPolicyBypassRequestSecurityOption2$Outbound, RevokeRiskPolicyBypassRequestSecurityOption2>;
export declare function revokeRiskPolicyBypassRequestSecurityOption2ToJSON(revokeRiskPolicyBypassRequestSecurityOption2: RevokeRiskPolicyBypassRequestSecurityOption2): string;
/** @internal */
export type RevokeRiskPolicyBypassRequestSecurity$Outbound = {
    Option1?: RevokeRiskPolicyBypassRequestSecurityOption1$Outbound | undefined;
    Option2?: RevokeRiskPolicyBypassRequestSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const RevokeRiskPolicyBypassRequestSecurity$outboundSchema: z.ZodMiniType<RevokeRiskPolicyBypassRequestSecurity$Outbound, RevokeRiskPolicyBypassRequestSecurity>;
export declare function revokeRiskPolicyBypassRequestSecurityToJSON(revokeRiskPolicyBypassRequestSecurity: RevokeRiskPolicyBypassRequestSecurity): string;
/** @internal */
export type RevokeRiskPolicyBypassRequestRequest$Outbound = {
    "Gram-Key"?: string | undefined;
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
    RiskIDRequestBody: RiskIDRequestBody$Outbound;
};
/** @internal */
export declare const RevokeRiskPolicyBypassRequestRequest$outboundSchema: z.ZodMiniType<RevokeRiskPolicyBypassRequestRequest$Outbound, RevokeRiskPolicyBypassRequestRequest>;
export declare function revokeRiskPolicyBypassRequestRequestToJSON(revokeRiskPolicyBypassRequestRequest: RevokeRiskPolicyBypassRequestRequest): string;
//# sourceMappingURL=revokeriskpolicybypassrequest.d.ts.map