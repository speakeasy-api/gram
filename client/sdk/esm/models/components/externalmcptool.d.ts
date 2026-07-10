import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type ExternalMCPTool = {
    /**
     * Annotations for the tool
     */
    annotations?: any | undefined;
    /**
     * Description of the tool
     */
    description?: string | undefined;
    /**
     * Input schema for the tool
     */
    inputSchema?: any | undefined;
    /**
     * Name of the tool
     */
    name?: string | undefined;
};
/** @internal */
export declare const ExternalMCPTool$inboundSchema: z.ZodMiniType<ExternalMCPTool, unknown>;
export declare function externalMCPToolFromJSON(jsonString: string): SafeParseResult<ExternalMCPTool, SDKValidationError>;
//# sourceMappingURL=externalmcptool.d.ts.map