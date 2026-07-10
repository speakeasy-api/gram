import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import { CustomDomain } from "../models/components/customdomain.js";
import { ListCustomDomainMcpEndpointsResult } from "../models/components/listcustomdomainmcpendpointsresult.js";
import {
  DeleteDomainRequest,
  DeleteDomainSecurity,
} from "../models/operations/deletedomain.js";
import {
  GetDomainRequest,
  GetDomainSecurity,
} from "../models/operations/getdomain.js";
import {
  ListCustomDomainMcpEndpointsRequest,
  ListCustomDomainMcpEndpointsSecurity,
} from "../models/operations/listcustomdomainmcpendpoints.js";
import {
  RegisterDomainRequest,
  RegisterDomainSecurity,
} from "../models/operations/registerdomain.js";
import {
  UpdateDomainRequest,
  UpdateDomainSecurity,
} from "../models/operations/updatedomain.js";
export declare class Domains extends ClientSDK {
  /**
   * deleteDomain domains
   *
   * @remarks
   * Delete a custom domain
   */
  deleteDomain(
    request?: DeleteDomainRequest | undefined,
    security?: DeleteDomainSecurity | undefined,
    options?: RequestOptions,
  ): Promise<void>;
  /**
   * getDomain domains
   *
   * @remarks
   * Get the custom domain for an organization
   */
  getDomain(
    request?: GetDomainRequest | undefined,
    security?: GetDomainSecurity | undefined,
    options?: RequestOptions,
  ): Promise<CustomDomain>;
  /**
   * listMcpEndpoints domains
   *
   * @remarks
   * List the MCP endpoints registered under the organization's custom domain across every project. Returns enriched rows that include the parent MCP server and project so callers can preview what a custom-domain deletion would cascade through.
   */
  listMcpEndpoints(
    request?: ListCustomDomainMcpEndpointsRequest | undefined,
    security?: ListCustomDomainMcpEndpointsSecurity | undefined,
    options?: RequestOptions,
  ): Promise<ListCustomDomainMcpEndpointsResult>;
  /**
   * createDomain domains
   *
   * @remarks
   * Create a custom domain for an organization
   */
  registerDomain(
    request: RegisterDomainRequest,
    security?: RegisterDomainSecurity | undefined,
    options?: RequestOptions,
  ): Promise<void>;
  /**
   * updateDomain domains
   *
   * @remarks
   * Update the IP allowlist for the organization's custom domain
   */
  updateDomain(
    request: UpdateDomainRequest,
    security?: UpdateDomainSecurity | undefined,
    options?: RequestOptions,
  ): Promise<CustomDomain>;
}
//# sourceMappingURL=domains.d.ts.map
