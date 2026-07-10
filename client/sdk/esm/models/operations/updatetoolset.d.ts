import * as z from "zod/v4-mini";
import { UpdateToolsetRequestBody, UpdateToolsetRequestBody$Outbound } from "../components/updatetoolsetrequestbody.js";
export type UpdateToolsetSecurityOption1 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type UpdateToolsetSecurityOption2 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type UpdateToolsetSecurity = {
    option1?: UpdateToolsetSecurityOption1 | undefined;
    option2?: UpdateToolsetSecurityOption2 | undefined;
};
export type UpdateToolsetRequest = {
    /**
     * The slug of the toolset to update
     */
    slug: string;
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
    updateToolsetRequestBody: UpdateToolsetRequestBody;
};
/** @internal */
export type UpdateToolsetSecurityOption1$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const UpdateToolsetSecurityOption1$outboundSchema: z.ZodMiniType<UpdateToolsetSecurityOption1$Outbound, UpdateToolsetSecurityOption1>;
export declare function updateToolsetSecurityOption1ToJSON(updateToolsetSecurityOption1: UpdateToolsetSecurityOption1): string;
/** @internal */
export type UpdateToolsetSecurityOption2$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const UpdateToolsetSecurityOption2$outboundSchema: z.ZodMiniType<UpdateToolsetSecurityOption2$Outbound, UpdateToolsetSecurityOption2>;
export declare function updateToolsetSecurityOption2ToJSON(updateToolsetSecurityOption2: UpdateToolsetSecurityOption2): string;
/** @internal */
export type UpdateToolsetSecurity$Outbound = {
    Option1?: UpdateToolsetSecurityOption1$Outbound | undefined;
    Option2?: UpdateToolsetSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const UpdateToolsetSecurity$outboundSchema: z.ZodMiniType<UpdateToolsetSecurity$Outbound, UpdateToolsetSecurity>;
export declare function updateToolsetSecurityToJSON(updateToolsetSecurity: UpdateToolsetSecurity): string;
/** @internal */
export type UpdateToolsetRequest$Outbound = {
    slug: string;
    "Gram-Session"?: string | undefined;
    "Gram-Key"?: string | undefined;
    "Gram-Project"?: string | undefined;
    UpdateToolsetRequestBody: UpdateToolsetRequestBody$Outbound;
};
/** @internal */
export declare const UpdateToolsetRequest$outboundSchema: z.ZodMiniType<UpdateToolsetRequest$Outbound, UpdateToolsetRequest>;
export declare function updateToolsetRequestToJSON(updateToolsetRequest: UpdateToolsetRequest): string;
//# sourceMappingURL=updatetoolset.d.ts.map