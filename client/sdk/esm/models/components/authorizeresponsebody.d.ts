import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type AuthorizeResponseBody = {
    /**
     * The opaque one-time code. Hand this to the device agent, which redeems it with its code_verifier.
     */
    code: string;
    /**
     * Lifetime of the code in seconds.
     */
    expiresIn: number;
};
/** @internal */
export declare const AuthorizeResponseBody$inboundSchema: z.ZodMiniType<AuthorizeResponseBody, unknown>;
export declare function authorizeResponseBodyFromJSON(jsonString: string): SafeParseResult<AuthorizeResponseBody, SDKValidationError>;
//# sourceMappingURL=authorizeresponsebody.d.ts.map