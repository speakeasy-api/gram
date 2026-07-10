import * as z from "zod/v4-mini";
import { AuthorizeRequestBody, AuthorizeRequestBody$Outbound } from "../components/authorizerequestbody.js";
export type CliAuthAuthorizeSecurity = {
    sessionHeaderGramSession?: string | undefined;
};
export type CliAuthAuthorizeRequest = {
    /**
     * Session header
     */
    gramSession?: string | undefined;
    authorizeRequestBody: AuthorizeRequestBody;
};
/** @internal */
export type CliAuthAuthorizeSecurity$Outbound = {
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const CliAuthAuthorizeSecurity$outboundSchema: z.ZodMiniType<CliAuthAuthorizeSecurity$Outbound, CliAuthAuthorizeSecurity>;
export declare function cliAuthAuthorizeSecurityToJSON(cliAuthAuthorizeSecurity: CliAuthAuthorizeSecurity): string;
/** @internal */
export type CliAuthAuthorizeRequest$Outbound = {
    "Gram-Session"?: string | undefined;
    AuthorizeRequestBody: AuthorizeRequestBody$Outbound;
};
/** @internal */
export declare const CliAuthAuthorizeRequest$outboundSchema: z.ZodMiniType<CliAuthAuthorizeRequest$Outbound, CliAuthAuthorizeRequest>;
export declare function cliAuthAuthorizeRequestToJSON(cliAuthAuthorizeRequest: CliAuthAuthorizeRequest): string;
//# sourceMappingURL=cliauthauthorize.d.ts.map