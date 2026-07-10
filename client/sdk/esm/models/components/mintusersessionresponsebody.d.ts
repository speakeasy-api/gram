import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type MintUserSessionResponseBody = {
    /**
     * The minted user-session JWT. Send as `Authorization: Bearer` on MCP requests to the bound /mcp/{slug} (or /x/mcp/{slug}) surface.
     */
    accessToken: string;
    /**
     * Lifetime of the access token in seconds.
     */
    expiresIn: number;
};
/** @internal */
export declare const MintUserSessionResponseBody$inboundSchema: z.ZodMiniType<MintUserSessionResponseBody, unknown>;
export declare function mintUserSessionResponseBodyFromJSON(jsonString: string): SafeParseResult<MintUserSessionResponseBody, SDKValidationError>;
//# sourceMappingURL=mintusersessionresponsebody.d.ts.map