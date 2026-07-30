import * as z from "zod";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type SetIterationModeRequestBody = {
  /**
   * Whether to enable iteration mode
   */
  iterationMode: boolean;
};
/** @internal */
export declare const SetIterationModeRequestBody$inboundSchema: z.ZodType<
  SetIterationModeRequestBody,
  z.ZodTypeDef,
  unknown
>;
/** @internal */
export type SetIterationModeRequestBody$Outbound = {
  iteration_mode: boolean;
};
/** @internal */
export declare const SetIterationModeRequestBody$outboundSchema: z.ZodType<
  SetIterationModeRequestBody$Outbound,
  z.ZodTypeDef,
  SetIterationModeRequestBody
>;
/**
 * @internal
 * @deprecated This namespace will be removed in future versions. Use schemas and types that are exported directly from this module.
 */
export declare namespace SetIterationModeRequestBody$ {
  /** @deprecated use `SetIterationModeRequestBody$inboundSchema` instead. */
  const inboundSchema: z.ZodType<
    SetIterationModeRequestBody,
    z.ZodTypeDef,
    unknown
  >;
  /** @deprecated use `SetIterationModeRequestBody$outboundSchema` instead. */
  const outboundSchema: z.ZodType<
    SetIterationModeRequestBody$Outbound,
    z.ZodTypeDef,
    SetIterationModeRequestBody
  >;
  /** @deprecated use `SetIterationModeRequestBody$Outbound` instead. */
  type Outbound = SetIterationModeRequestBody$Outbound;
}
export declare function setIterationModeRequestBodyToJSON(
  setIterationModeRequestBody: SetIterationModeRequestBody,
): string;
export declare function setIterationModeRequestBodyFromJSON(
  jsonString: string,
): SafeParseResult<SetIterationModeRequestBody, SDKValidationError>;
//# sourceMappingURL=setiterationmoderequestbody.d.ts.map
