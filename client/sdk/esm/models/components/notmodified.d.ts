import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type NotModified = {
    location: string;
};
/** @internal */
export declare const NotModified$inboundSchema: z.ZodMiniType<NotModified, unknown>;
export declare function notModifiedFromJSON(jsonString: string): SafeParseResult<NotModified, SDKValidationError>;
//# sourceMappingURL=notmodified.d.ts.map