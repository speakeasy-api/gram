import * as z from "zod/v4-mini";
import { SetSourceEnvironmentLinkRequestBody, SetSourceEnvironmentLinkRequestBody$Outbound } from "../components/setsourceenvironmentlinkrequestbody.js";
export type SetSourceEnvironmentLinkSecurity = {
    projectSlugHeaderGramProject?: string | undefined;
    sessionHeaderGramSession?: string | undefined;
};
export type SetSourceEnvironmentLinkRequest = {
    /**
     * Session header
     */
    gramSession?: string | undefined;
    /**
     * project header
     */
    gramProject?: string | undefined;
    setSourceEnvironmentLinkRequestBody: SetSourceEnvironmentLinkRequestBody;
};
/** @internal */
export type SetSourceEnvironmentLinkSecurity$Outbound = {
    "project_slug_header_Gram-Project"?: string | undefined;
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const SetSourceEnvironmentLinkSecurity$outboundSchema: z.ZodMiniType<SetSourceEnvironmentLinkSecurity$Outbound, SetSourceEnvironmentLinkSecurity>;
export declare function setSourceEnvironmentLinkSecurityToJSON(setSourceEnvironmentLinkSecurity: SetSourceEnvironmentLinkSecurity): string;
/** @internal */
export type SetSourceEnvironmentLinkRequest$Outbound = {
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
    SetSourceEnvironmentLinkRequestBody: SetSourceEnvironmentLinkRequestBody$Outbound;
};
/** @internal */
export declare const SetSourceEnvironmentLinkRequest$outboundSchema: z.ZodMiniType<SetSourceEnvironmentLinkRequest$Outbound, SetSourceEnvironmentLinkRequest>;
export declare function setSourceEnvironmentLinkRequestToJSON(setSourceEnvironmentLinkRequest: SetSourceEnvironmentLinkRequest): string;
//# sourceMappingURL=setsourceenvironmentlink.d.ts.map