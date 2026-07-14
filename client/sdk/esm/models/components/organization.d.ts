import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type Organization = {
  /**
   * The account type of the organization
   */
  accountType: string;
  /**
   * The creation date of the organization.
   */
  createdAt: Date;
  /**
   * The ID of the organization
   */
  id: string;
  /**
   * The name of the organization
   */
  name: string;
  /**
   * A short url-friendly label that uniquely identifies a resource.
   */
  slug: string;
  /**
   * The last update date of the organization.
   */
  updatedAt: Date;
  /**
   * Whether webhooks are enabled for the organization
   */
  webhooksEnabled: boolean;
  /**
   * Whether webhooks support is initialized for the organization
   */
  webhooksOnboarded: boolean;
};
/** @internal */
export declare const Organization$inboundSchema: z.ZodMiniType<
  Organization,
  unknown
>;
export declare function organizationFromJSON(
  jsonString: string,
): SafeParseResult<Organization, SDKValidationError>;
//# sourceMappingURL=organization.d.ts.map
