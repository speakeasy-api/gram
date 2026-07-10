import * as z from "zod/v4-mini";
import { RedeployRequestBody, RedeployRequestBody$Outbound } from "../components/redeployrequestbody.js";
export type RedeployDeploymentSecurityOption1 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type RedeployDeploymentSecurityOption2 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type RedeployDeploymentSecurity = {
    option1?: RedeployDeploymentSecurityOption1 | undefined;
    option2?: RedeployDeploymentSecurityOption2 | undefined;
};
export type RedeployDeploymentRequest = {
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
    redeployRequestBody: RedeployRequestBody;
};
/** @internal */
export type RedeployDeploymentSecurityOption1$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const RedeployDeploymentSecurityOption1$outboundSchema: z.ZodMiniType<RedeployDeploymentSecurityOption1$Outbound, RedeployDeploymentSecurityOption1>;
export declare function redeployDeploymentSecurityOption1ToJSON(redeployDeploymentSecurityOption1: RedeployDeploymentSecurityOption1): string;
/** @internal */
export type RedeployDeploymentSecurityOption2$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const RedeployDeploymentSecurityOption2$outboundSchema: z.ZodMiniType<RedeployDeploymentSecurityOption2$Outbound, RedeployDeploymentSecurityOption2>;
export declare function redeployDeploymentSecurityOption2ToJSON(redeployDeploymentSecurityOption2: RedeployDeploymentSecurityOption2): string;
/** @internal */
export type RedeployDeploymentSecurity$Outbound = {
    Option1?: RedeployDeploymentSecurityOption1$Outbound | undefined;
    Option2?: RedeployDeploymentSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const RedeployDeploymentSecurity$outboundSchema: z.ZodMiniType<RedeployDeploymentSecurity$Outbound, RedeployDeploymentSecurity>;
export declare function redeployDeploymentSecurityToJSON(redeployDeploymentSecurity: RedeployDeploymentSecurity): string;
/** @internal */
export type RedeployDeploymentRequest$Outbound = {
    "Gram-Key"?: string | undefined;
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
    RedeployRequestBody: RedeployRequestBody$Outbound;
};
/** @internal */
export declare const RedeployDeploymentRequest$outboundSchema: z.ZodMiniType<RedeployDeploymentRequest$Outbound, RedeployDeploymentRequest>;
export declare function redeployDeploymentRequestToJSON(redeployDeploymentRequest: RedeployDeploymentRequest): string;
//# sourceMappingURL=redeploydeployment.d.ts.map