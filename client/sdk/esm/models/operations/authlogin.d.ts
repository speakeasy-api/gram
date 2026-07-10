import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type AuthLoginRequest = {
    /**
     * Optional URL to redirect to after successful authentication
     */
    redirect?: string | undefined;
};
export type AuthLoginResponse = {
    headers: {
        [k: string]: Array<string>;
    };
};
/** @internal */
export type AuthLoginRequest$Outbound = {
    redirect?: string | undefined;
};
/** @internal */
export declare const AuthLoginRequest$outboundSchema: z.ZodMiniType<AuthLoginRequest$Outbound, AuthLoginRequest>;
export declare function authLoginRequestToJSON(authLoginRequest: AuthLoginRequest): string;
/** @internal */
export declare const AuthLoginResponse$inboundSchema: z.ZodMiniType<AuthLoginResponse, unknown>;
export declare function authLoginResponseFromJSON(jsonString: string): SafeParseResult<AuthLoginResponse, SDKValidationError>;
//# sourceMappingURL=authlogin.d.ts.map