import * as z from "zod/v4-mini";
import { CreateRiskPolicyRequestBody, CreateRiskPolicyRequestBody$Outbound } from "../components/createriskpolicyrequestbody.js";
export type CreateRiskPolicySecurityOption1 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type CreateRiskPolicySecurityOption2 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type CreateRiskPolicySecurity = {
    option1?: CreateRiskPolicySecurityOption1 | undefined;
    option2?: CreateRiskPolicySecurityOption2 | undefined;
};
export type CreateRiskPolicyRequest = {
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
    createRiskPolicyRequestBody: CreateRiskPolicyRequestBody;
};
/** @internal */
export type CreateRiskPolicySecurityOption1$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const CreateRiskPolicySecurityOption1$outboundSchema: z.ZodMiniType<CreateRiskPolicySecurityOption1$Outbound, CreateRiskPolicySecurityOption1>;
export declare function createRiskPolicySecurityOption1ToJSON(createRiskPolicySecurityOption1: CreateRiskPolicySecurityOption1): string;
/** @internal */
export type CreateRiskPolicySecurityOption2$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const CreateRiskPolicySecurityOption2$outboundSchema: z.ZodMiniType<CreateRiskPolicySecurityOption2$Outbound, CreateRiskPolicySecurityOption2>;
export declare function createRiskPolicySecurityOption2ToJSON(createRiskPolicySecurityOption2: CreateRiskPolicySecurityOption2): string;
/** @internal */
export type CreateRiskPolicySecurity$Outbound = {
    Option1?: CreateRiskPolicySecurityOption1$Outbound | undefined;
    Option2?: CreateRiskPolicySecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const CreateRiskPolicySecurity$outboundSchema: z.ZodMiniType<CreateRiskPolicySecurity$Outbound, CreateRiskPolicySecurity>;
export declare function createRiskPolicySecurityToJSON(createRiskPolicySecurity: CreateRiskPolicySecurity): string;
/** @internal */
export type CreateRiskPolicyRequest$Outbound = {
    "Gram-Key"?: string | undefined;
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
    CreateRiskPolicyRequestBody: CreateRiskPolicyRequestBody$Outbound;
};
/** @internal */
export declare const CreateRiskPolicyRequest$outboundSchema: z.ZodMiniType<CreateRiskPolicyRequest$Outbound, CreateRiskPolicyRequest>;
export declare function createRiskPolicyRequestToJSON(createRiskPolicyRequest: CreateRiskPolicyRequest): string;
//# sourceMappingURL=createriskpolicy.d.ts.map