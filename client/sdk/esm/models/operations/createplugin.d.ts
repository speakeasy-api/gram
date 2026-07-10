import * as z from "zod/v4-mini";
import { CreatePluginForm, CreatePluginForm$Outbound } from "../components/createpluginform.js";
export type CreatePluginSecurity = {
    projectSlugHeaderGramProject?: string | undefined;
    sessionHeaderGramSession?: string | undefined;
};
export type CreatePluginRequest = {
    /**
     * Session header
     */
    gramSession?: string | undefined;
    /**
     * project header
     */
    gramProject?: string | undefined;
    createPluginForm: CreatePluginForm;
};
/** @internal */
export type CreatePluginSecurity$Outbound = {
    "project_slug_header_Gram-Project"?: string | undefined;
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const CreatePluginSecurity$outboundSchema: z.ZodMiniType<CreatePluginSecurity$Outbound, CreatePluginSecurity>;
export declare function createPluginSecurityToJSON(createPluginSecurity: CreatePluginSecurity): string;
/** @internal */
export type CreatePluginRequest$Outbound = {
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
    CreatePluginForm: CreatePluginForm$Outbound;
};
/** @internal */
export declare const CreatePluginRequest$outboundSchema: z.ZodMiniType<CreatePluginRequest$Outbound, CreatePluginRequest>;
export declare function createPluginRequestToJSON(createPluginRequest: CreatePluginRequest): string;
//# sourceMappingURL=createplugin.d.ts.map