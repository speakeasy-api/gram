import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type RiskCapabilitiesResult = {
    /**
     * Whether the prompt-injection ML classifier is configured on this server.
     */
    piClassifierEnabled: boolean;
};
/** @internal */
export declare const RiskCapabilitiesResult$inboundSchema: z.ZodMiniType<RiskCapabilitiesResult, unknown>;
export declare function riskCapabilitiesResultFromJSON(jsonString: string): SafeParseResult<RiskCapabilitiesResult, SDKValidationError>;
//# sourceMappingURL=riskcapabilitiesresult.d.ts.map