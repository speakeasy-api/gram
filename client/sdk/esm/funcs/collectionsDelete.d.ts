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
  DeleteCollectionRequest,
  DeleteCollectionSecurity,
} from "../models/operations/deletecollection.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * delete collections
 *
 * @remarks
 * Delete an MCP collection
 */
export declare function collectionsDelete(
  client: GramCore,
  request: DeleteCollectionRequest,
  security?: DeleteCollectionSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    void,
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
//# sourceMappingURL=collectionsDelete.d.ts.map
