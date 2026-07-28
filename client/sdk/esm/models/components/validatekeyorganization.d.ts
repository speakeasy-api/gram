import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type ValidateKeyOrganization = {
  /**
   * The ID of the organization
   */
  id: string;
  /**
   * The name of the organization
   */
  name: string;
  /**
   * The slug of the organization
   */
  slug: string;
};
/** @internal */
export declare const ValidateKeyOrganization$inboundSchema: z.ZodMiniType<
  ValidateKeyOrganization,
  unknown
>;
export declare function validateKeyOrganizationFromJSON(
  jsonString: string,
): SafeParseResult<ValidateKeyOrganization, SDKValidationError>;
//# sourceMappingURL=validatekeyorganization.d.ts.map
