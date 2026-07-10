import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type RiskPolicyModelConfig = {
  /**
   * When the judge errors or times out: true allows the message (fail-open), false blocks it (fail-closed). Defaults to fail-open.
   */
  failOpen?: boolean | undefined;
  /**
   * OpenRouter model id the judge should use. Empty selects the default judge model.
   */
  model?: string | undefined;
  /**
   * Sampling temperature for the judge. Defaults to a low value for deterministic verdicts.
   */
  temperature?: number | undefined;
};
/** @internal */
export declare const RiskPolicyModelConfig$inboundSchema: z.ZodMiniType<
  RiskPolicyModelConfig,
  unknown
>;
/** @internal */
export type RiskPolicyModelConfig$Outbound = {
  fail_open?: boolean | undefined;
  model?: string | undefined;
  temperature?: number | undefined;
};
/** @internal */
export declare const RiskPolicyModelConfig$outboundSchema: z.ZodMiniType<
  RiskPolicyModelConfig$Outbound,
  RiskPolicyModelConfig
>;
export declare function riskPolicyModelConfigToJSON(
  riskPolicyModelConfig: RiskPolicyModelConfig,
): string;
export declare function riskPolicyModelConfigFromJSON(
  jsonString: string,
): SafeParseResult<RiskPolicyModelConfig, SDKValidationError>;
//# sourceMappingURL=riskpolicymodelconfig.d.ts.map
