import * as z from "zod/v4-mini";
import { SetToolsetEnvironmentLinkRequestBody, SetToolsetEnvironmentLinkRequestBody$Outbound } from "../components/settoolsetenvironmentlinkrequestbody.js";
export type SetToolsetEnvironmentLinkSecurity = {
    projectSlugHeaderGramProject?: string | undefined;
    sessionHeaderGramSession?: string | undefined;
};
export type SetToolsetEnvironmentLinkRequest = {
    /**
     * Session header
     */
    gramSession?: string | undefined;
    /**
     * project header
     */
    gramProject?: string | undefined;
    setToolsetEnvironmentLinkRequestBody: SetToolsetEnvironmentLinkRequestBody;
};
/** @internal */
export type SetToolsetEnvironmentLinkSecurity$Outbound = {
    "project_slug_header_Gram-Project"?: string | undefined;
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const SetToolsetEnvironmentLinkSecurity$outboundSchema: z.ZodMiniType<SetToolsetEnvironmentLinkSecurity$Outbound, SetToolsetEnvironmentLinkSecurity>;
export declare function setToolsetEnvironmentLinkSecurityToJSON(setToolsetEnvironmentLinkSecurity: SetToolsetEnvironmentLinkSecurity): string;
/** @internal */
export type SetToolsetEnvironmentLinkRequest$Outbound = {
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
    SetToolsetEnvironmentLinkRequestBody: SetToolsetEnvironmentLinkRequestBody$Outbound;
};
/** @internal */
export declare const SetToolsetEnvironmentLinkRequest$outboundSchema: z.ZodMiniType<SetToolsetEnvironmentLinkRequest$Outbound, SetToolsetEnvironmentLinkRequest>;
export declare function setToolsetEnvironmentLinkRequestToJSON(setToolsetEnvironmentLinkRequest: SetToolsetEnvironmentLinkRequest): string;
//# sourceMappingURL=settoolsetenvironmentlink.d.ts.map