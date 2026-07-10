import * as z from "zod/v4-mini";
import { UpdatePluginServerForm, UpdatePluginServerForm$Outbound } from "../components/updatepluginserverform.js";
export type UpdatePluginServerSecurity = {
    projectSlugHeaderGramProject?: string | undefined;
    sessionHeaderGramSession?: string | undefined;
};
export type UpdatePluginServerRequest = {
    /**
     * Session header
     */
    gramSession?: string | undefined;
    /**
     * project header
     */
    gramProject?: string | undefined;
    updatePluginServerForm: UpdatePluginServerForm;
};
/** @internal */
export type UpdatePluginServerSecurity$Outbound = {
    "project_slug_header_Gram-Project"?: string | undefined;
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const UpdatePluginServerSecurity$outboundSchema: z.ZodMiniType<UpdatePluginServerSecurity$Outbound, UpdatePluginServerSecurity>;
export declare function updatePluginServerSecurityToJSON(updatePluginServerSecurity: UpdatePluginServerSecurity): string;
/** @internal */
export type UpdatePluginServerRequest$Outbound = {
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
    UpdatePluginServerForm: UpdatePluginServerForm$Outbound;
};
/** @internal */
export declare const UpdatePluginServerRequest$outboundSchema: z.ZodMiniType<UpdatePluginServerRequest$Outbound, UpdatePluginServerRequest>;
export declare function updatePluginServerRequestToJSON(updatePluginServerRequest: UpdatePluginServerRequest): string;
//# sourceMappingURL=updatepluginserver.d.ts.map