import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * A single environment entry
 */
export type EnvironmentEntry = {
  /**
   * The creation date of the environment entry
   */
  createdAt: Date;
  /**
   * The name of the environment variable
   */
  name: string;
  /**
   * When the environment entry was last updated
   */
  updatedAt: Date;
  /**
   * Redacted values of the environment variable
   */
  value: string;
};
/** @internal */
export declare const EnvironmentEntry$inboundSchema: z.ZodMiniType<
  EnvironmentEntry,
  unknown
>;
export declare function environmentEntryFromJSON(
  jsonString: string,
): SafeParseResult<EnvironmentEntry, SDKValidationError>;
//# sourceMappingURL=environmententry.d.ts.map
