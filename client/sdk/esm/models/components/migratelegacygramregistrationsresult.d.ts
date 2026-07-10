import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Result of a legacy gram registration migration.
 */
export type MigrateLegacyGramRegistrationsResult = {
  /**
   * Number of user_session_clients newly inserted; already-migrated registrations count as zero.
   */
  migratedCount: number;
};
/** @internal */
export declare const MigrateLegacyGramRegistrationsResult$inboundSchema: z.ZodMiniType<
  MigrateLegacyGramRegistrationsResult,
  unknown
>;
export declare function migrateLegacyGramRegistrationsResultFromJSON(
  jsonString: string,
): SafeParseResult<MigrateLegacyGramRegistrationsResult, SDKValidationError>;
//# sourceMappingURL=migratelegacygramregistrationsresult.d.ts.map
