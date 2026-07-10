import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListResponseBody } from "../models/components/listresponsebody.js";
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
  ListCollectionsRequest,
  ListCollectionsSecurity,
} from "../models/operations/listcollections.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * list collections
 *
 * @remarks
 * List MCP collections in the organization
 */
export declare function collectionsList(
  client: GramCore,
  request?: ListCollectionsRequest | undefined,
  security?: ListCollectionsSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    ListResponseBody,
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
//# sourceMappingURL=collectionsList.d.ts.map
