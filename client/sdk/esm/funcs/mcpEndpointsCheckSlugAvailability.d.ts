import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import {
  ConnectionError,
  InvalidRequestError,
  RequestAbortedError,
  RequestTimeoutError,
  UnexpectedClientError,
} from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import {
  CheckMcpEndpointSlugAvailabilityRequest,
  CheckMcpEndpointSlugAvailabilitySecurity,
} from "../models/operations/checkmcpendpointslugavailability.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * checkMcpEndpointSlugAvailability mcpEndpoints
 *
 * @remarks
 * Check whether an MCP endpoint slug is available. The uniqueness scope depends on whether a custom_domain_id is provided: platform-domain slugs are checked across all platform-domain endpoints (custom_domain_id IS NULL); custom-domain slugs are checked within the (custom_domain_id, slug) pair. Returns true when the slug is free.
 */
export declare function mcpEndpointsCheckSlugAvailability(
  client: GramCore,
  request: CheckMcpEndpointSlugAvailabilityRequest,
  security?: CheckMcpEndpointSlugAvailabilitySecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    boolean,
    | ServiceError
    | GramError
    | ResponseValidationError
    | ConnectionError
    | RequestAbortedError
    | RequestTimeoutError
    | InvalidRequestError
    | UnexpectedClientError
    | SDKValidationError
  >
>;
//# sourceMappingURL=mcpEndpointsCheckSlugAvailability.d.ts.map
