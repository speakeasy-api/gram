import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type GenerateTitleResponseBody = {
    /**
     * The current title after the operation (empty when reset to auto-generated)
     */
    title: string;
};
/** @internal */
export declare const GenerateTitleResponseBody$inboundSchema: z.ZodMiniType<GenerateTitleResponseBody, unknown>;
export declare function generateTitleResponseBodyFromJSON(jsonString: string): SafeParseResult<GenerateTitleResponseBody, SDKValidationError>;
//# sourceMappingURL=generatetitleresponsebody.d.ts.map