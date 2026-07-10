import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type CustomDomain = {
  /**
   * Whether the domain is activated in ingress
   */
  activated: boolean;
  /**
   * When the custom domain was created.
   */
  createdAt: Date;
  /**
   * The custom domain name
   */
  domain: string;
  /**
   * The ID of the custom domain
   */
  id: string;
  /**
   * IP addresses or CIDR ranges allowed to access this domain. Empty list means unrestricted.
   */
  ipAllowlist: Array<string>;
  /**
   * The custom domain is actively being registered
   */
  isUpdating: boolean;
  /**
   * The ID of the organization this domain belongs to
   */
  organizationId: string;
  /**
   * When the custom domain was last updated.
   */
  updatedAt: Date;
  /**
   * Whether the domain is verified
   */
  verified: boolean;
};
/** @internal */
export declare const CustomDomain$inboundSchema: z.ZodMiniType<
  CustomDomain,
  unknown
>;
export declare function customDomainFromJSON(
  jsonString: string,
): SafeParseResult<CustomDomain, SDKValidationError>;
//# sourceMappingURL=customdomain.d.ts.map
