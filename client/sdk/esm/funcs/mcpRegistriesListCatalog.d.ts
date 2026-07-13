import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListCatalogResponseBody } from "../models/components/listcatalogresponsebody.js";
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
  ListMCPCatalogRequest,
  ListMCPCatalogSecurity,
} from "../models/operations/listmcpcatalog.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * listCatalog mcpRegistries
 *
 * @remarks
 * List available MCP servers from configured registries
 */
export declare function mcpRegistriesListCatalog(
  client: GramCore,
  request?: ListMCPCatalogRequest | undefined,
  security?: ListMCPCatalogSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    ListCatalogResponseBody,
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
//# sourceMappingURL=mcpRegistriesListCatalog.d.ts.map
