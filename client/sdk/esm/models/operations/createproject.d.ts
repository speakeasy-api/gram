import * as z from "zod/v4-mini";
import { CreateProjectRequestBody, CreateProjectRequestBody$Outbound } from "../components/createprojectrequestbody.js";
export type CreateProjectSecurity = {
    apikeyHeaderGramKey?: string | undefined;
    sessionHeaderGramSession?: string | undefined;
};
export type CreateProjectRequest = {
    /**
     * API Key header
     */
    gramKey?: string | undefined;
    /**
     * Session header
     */
    gramSession?: string | undefined;
    createProjectRequestBody: CreateProjectRequestBody;
};
/** @internal */
export type CreateProjectSecurity$Outbound = {
    "apikey_header_Gram-Key"?: string | undefined;
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const CreateProjectSecurity$outboundSchema: z.ZodMiniType<CreateProjectSecurity$Outbound, CreateProjectSecurity>;
export declare function createProjectSecurityToJSON(createProjectSecurity: CreateProjectSecurity): string;
/** @internal */
export type CreateProjectRequest$Outbound = {
    "Gram-Key"?: string | undefined;
    "Gram-Session"?: string | undefined;
    CreateProjectRequestBody: CreateProjectRequestBody$Outbound;
};
/** @internal */
export declare const CreateProjectRequest$outboundSchema: z.ZodMiniType<CreateProjectRequest$Outbound, CreateProjectRequest>;
export declare function createProjectRequestToJSON(createProjectRequest: CreateProjectRequest): string;
//# sourceMappingURL=createproject.d.ts.map