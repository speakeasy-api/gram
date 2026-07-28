import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListRegistriesResponseBody } from "../models/components/listregistriesresponsebody.js";
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
  ListMCPRegistriesRequest,
  ListMCPRegistriesSecurity,
} from "../models/operations/listmcpregistries.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * listRegistries mcpRegistries
 *
 * @remarks
 * List all MCP registries (admin only)
 */
export declare function mcpRegistriesListRegistries(
  client: GramCore,
  request?: ListMCPRegistriesRequest | undefined,
  security?: ListMCPRegistriesSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    ListRegistriesResponseBody,
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
//# sourceMappingURL=mcpRegistriesListRegistries.d.ts.map
