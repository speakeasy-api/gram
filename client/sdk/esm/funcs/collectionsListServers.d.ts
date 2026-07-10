import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListServersResponseBody } from "../models/components/listserversresponsebody.js";
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
  ListCollectionServersRequest,
  ListCollectionServersSecurity,
} from "../models/operations/listcollectionservers.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * listServers collections
 *
 * @remarks
 * List published MCP servers from a collection
 */
export declare function collectionsListServers(
  client: GramCore,
  request: ListCollectionServersRequest,
  security?: ListCollectionServersSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    ListServersResponseBody,
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
//# sourceMappingURL=collectionsListServers.d.ts.map
