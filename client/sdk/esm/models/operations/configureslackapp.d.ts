import * as z from "zod/v4-mini";
import * as components from "../components/index.js";
export type ConfigureSlackAppSecurity = {
    projectSlugHeaderGramProject?: string | undefined;
    sessionHeaderGramSession?: string | undefined;
};
export type ConfigureSlackAppRequest = {
    /**
     * Session header
     */
    gramSession?: string | undefined;
    /**
     * project header
     */
    gramProject?: string | undefined;
    configureSlackAppRequestBody: components.ConfigureSlackAppRequestBody;
};
/** @internal */
export type ConfigureSlackAppSecurity$Outbound = {
    "project_slug_header_Gram-Project"?: string | undefined;
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const ConfigureSlackAppSecurity$outboundSchema: z.ZodMiniType<ConfigureSlackAppSecurity$Outbound, ConfigureSlackAppSecurity>;
export declare function configureSlackAppSecurityToJSON(configureSlackAppSecurity: ConfigureSlackAppSecurity): string;
/** @internal */
export type ConfigureSlackAppRequest$Outbound = {
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
    ConfigureSlackAppRequestBody: components.ConfigureSlackAppRequestBody$Outbound;
};
/** @internal */
export declare const ConfigureSlackAppRequest$outboundSchema: z.ZodMiniType<ConfigureSlackAppRequest$Outbound, ConfigureSlackAppRequest>;
export declare function configureSlackAppRequestToJSON(configureSlackAppRequest: ConfigureSlackAppRequest): string;
//# sourceMappingURL=configureslackapp.d.ts.map