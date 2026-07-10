import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Whether the local hook should allow or deny the action.
 */
export declare const Decision: {
  readonly Allow: "allow";
  readonly Deny: "deny";
};
/**
 * Whether the local hook should allow or deny the action.
 */
export type Decision = ClosedEnum<typeof Decision>;
/**
 * Provider-neutral decision returned by the unified hook endpoint.
 */
export type IngestHookResult = {
  /**
   * Whether the local hook should allow or deny the action.
   */
  decision: Decision;
  /**
   * Optional side-effect hints for hook SDKs.
   */
  effects?:
    | {
        [k: string]: any;
      }
    | undefined;
  /**
   * User-facing decision message.
   */
  message?: string | undefined;
  /**
   * Machine-readable decision reason.
   */
  reason?: string | undefined;
};
/** @internal */
export declare const Decision$inboundSchema: z.ZodMiniEnum<typeof Decision>;
/** @internal */
export declare const IngestHookResult$inboundSchema: z.ZodMiniType<
  IngestHookResult,
  unknown
>;
export declare function ingestHookResultFromJSON(
  jsonString: string,
): SafeParseResult<IngestHookResult, SDKValidationError>;
//# sourceMappingURL=ingesthookresult.d.ts.map
