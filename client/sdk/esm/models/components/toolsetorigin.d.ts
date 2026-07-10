import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type ToolsetOrigin = {
    /**
     * The globally unique registry specifier this toolset originated from
     */
    registrySpecifier: string;
};
/** @internal */
export declare const ToolsetOrigin$inboundSchema: z.ZodMiniType<ToolsetOrigin, unknown>;
/** @internal */
export type ToolsetOrigin$Outbound = {
    registry_specifier: string;
};
/** @internal */
export declare const ToolsetOrigin$outboundSchema: z.ZodMiniType<ToolsetOrigin$Outbound, ToolsetOrigin>;
export declare function toolsetOriginToJSON(toolsetOrigin: ToolsetOrigin): string;
export declare function toolsetOriginFromJSON(jsonString: string): SafeParseResult<ToolsetOrigin, SDKValidationError>;
//# sourceMappingURL=toolsetorigin.d.ts.map