import * as z from "zod/v4-mini";
import { CreateDeploymentRequestBody, CreateDeploymentRequestBody$Outbound } from "../components/createdeploymentrequestbody.js";
export type CreateDeploymentSecurityOption1 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type CreateDeploymentSecurityOption2 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type CreateDeploymentSecurity = {
    option1?: CreateDeploymentSecurityOption1 | undefined;
    option2?: CreateDeploymentSecurityOption2 | undefined;
};
export type CreateDeploymentRequest = {
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
    /**
     * A unique identifier that will mitigate against duplicate deployments.
     */
    idempotencyKey: string;
    createDeploymentRequestBody: CreateDeploymentRequestBody;
};
/** @internal */
export type CreateDeploymentSecurityOption1$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const CreateDeploymentSecurityOption1$outboundSchema: z.ZodMiniType<CreateDeploymentSecurityOption1$Outbound, CreateDeploymentSecurityOption1>;
export declare function createDeploymentSecurityOption1ToJSON(createDeploymentSecurityOption1: CreateDeploymentSecurityOption1): string;
/** @internal */
export type CreateDeploymentSecurityOption2$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const CreateDeploymentSecurityOption2$outboundSchema: z.ZodMiniType<CreateDeploymentSecurityOption2$Outbound, CreateDeploymentSecurityOption2>;
export declare function createDeploymentSecurityOption2ToJSON(createDeploymentSecurityOption2: CreateDeploymentSecurityOption2): string;
/** @internal */
export type CreateDeploymentSecurity$Outbound = {
    Option1?: CreateDeploymentSecurityOption1$Outbound | undefined;
    Option2?: CreateDeploymentSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const CreateDeploymentSecurity$outboundSchema: z.ZodMiniType<CreateDeploymentSecurity$Outbound, CreateDeploymentSecurity>;
export declare function createDeploymentSecurityToJSON(createDeploymentSecurity: CreateDeploymentSecurity): string;
/** @internal */
export type CreateDeploymentRequest$Outbound = {
    "Gram-Key"?: string | undefined;
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
    "Idempotency-Key": string;
    CreateDeploymentRequestBody: CreateDeploymentRequestBody$Outbound;
};
/** @internal */
export declare const CreateDeploymentRequest$outboundSchema: z.ZodMiniType<CreateDeploymentRequest$Outbound, CreateDeploymentRequest>;
export declare function createDeploymentRequestToJSON(createDeploymentRequest: CreateDeploymentRequest): string;
//# sourceMappingURL=createdeployment.d.ts.map