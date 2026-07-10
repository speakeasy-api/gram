import * as z from "zod/v4-mini";
import * as components from "../components/index.js";
export type CreateSlackAppSecurity = {
    projectSlugHeaderGramProject?: string | undefined;
    sessionHeaderGramSession?: string | undefined;
};
export type CreateSlackAppRequest = {
    /**
     * Session header
     */
    gramSession?: string | undefined;
    /**
     * project header
     */
    gramProject?: string | undefined;
    createSlackAppRequestBody: components.CreateSlackAppRequestBody;
};
/** @internal */
export type CreateSlackAppSecurity$Outbound = {
    "project_slug_header_Gram-Project"?: string | undefined;
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const CreateSlackAppSecurity$outboundSchema: z.ZodMiniType<CreateSlackAppSecurity$Outbound, CreateSlackAppSecurity>;
export declare function createSlackAppSecurityToJSON(createSlackAppSecurity: CreateSlackAppSecurity): string;
/** @internal */
export type CreateSlackAppRequest$Outbound = {
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
    CreateSlackAppRequestBody: components.CreateSlackAppRequestBody$Outbound;
};
/** @internal */
export declare const CreateSlackAppRequest$outboundSchema: z.ZodMiniType<CreateSlackAppRequest$Outbound, CreateSlackAppRequest>;
export declare function createSlackAppRequestToJSON(createSlackAppRequest: CreateSlackAppRequest): string;
//# sourceMappingURL=createslackapp.d.ts.map