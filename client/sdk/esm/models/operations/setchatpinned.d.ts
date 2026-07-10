import * as z from "zod/v4-mini";
import { SetPinnedRequestBody, SetPinnedRequestBody$Outbound } from "../components/setpinnedrequestbody.js";
export type SetChatPinnedSecurity = {
    projectSlugHeaderGramProject?: string | undefined;
    sessionHeaderGramSession?: string | undefined;
};
export type SetChatPinnedRequest = {
    /**
     * Session header
     */
    gramSession?: string | undefined;
    /**
     * project header
     */
    gramProject?: string | undefined;
    setPinnedRequestBody: SetPinnedRequestBody;
};
/** @internal */
export type SetChatPinnedSecurity$Outbound = {
    "project_slug_header_Gram-Project"?: string | undefined;
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const SetChatPinnedSecurity$outboundSchema: z.ZodMiniType<SetChatPinnedSecurity$Outbound, SetChatPinnedSecurity>;
export declare function setChatPinnedSecurityToJSON(setChatPinnedSecurity: SetChatPinnedSecurity): string;
/** @internal */
export type SetChatPinnedRequest$Outbound = {
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
    SetPinnedRequestBody: SetPinnedRequestBody$Outbound;
};
/** @internal */
export declare const SetChatPinnedRequest$outboundSchema: z.ZodMiniType<SetChatPinnedRequest$Outbound, SetChatPinnedRequest>;
export declare function setChatPinnedRequestToJSON(setChatPinnedRequest: SetChatPinnedRequest): string;
//# sourceMappingURL=setchatpinned.d.ts.map