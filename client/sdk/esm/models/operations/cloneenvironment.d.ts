import * as z from "zod/v4-mini";
import { CloneEnvironmentRequestBody, CloneEnvironmentRequestBody$Outbound } from "../components/cloneenvironmentrequestbody.js";
export type CloneEnvironmentSecurity = {
    projectSlugHeaderGramProject?: string | undefined;
    sessionHeaderGramSession?: string | undefined;
};
export type CloneEnvironmentRequest = {
    /**
     * The slug of the source environment to clone
     */
    slug: string;
    /**
     * Session header
     */
    gramSession?: string | undefined;
    /**
     * project header
     */
    gramProject?: string | undefined;
    cloneEnvironmentRequestBody: CloneEnvironmentRequestBody;
};
/** @internal */
export type CloneEnvironmentSecurity$Outbound = {
    "project_slug_header_Gram-Project"?: string | undefined;
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const CloneEnvironmentSecurity$outboundSchema: z.ZodMiniType<CloneEnvironmentSecurity$Outbound, CloneEnvironmentSecurity>;
export declare function cloneEnvironmentSecurityToJSON(cloneEnvironmentSecurity: CloneEnvironmentSecurity): string;
/** @internal */
export type CloneEnvironmentRequest$Outbound = {
    slug: string;
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
    CloneEnvironmentRequestBody: CloneEnvironmentRequestBody$Outbound;
};
/** @internal */
export declare const CloneEnvironmentRequest$outboundSchema: z.ZodMiniType<CloneEnvironmentRequest$Outbound, CloneEnvironmentRequest>;
export declare function cloneEnvironmentRequestToJSON(cloneEnvironmentRequest: CloneEnvironmentRequest): string;
//# sourceMappingURL=cloneenvironment.d.ts.map