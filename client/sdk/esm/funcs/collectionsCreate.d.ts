import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { MCPCollection } from "../models/components/mcpcollection.js";
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
  CreateCollectionRequest,
  CreateCollectionSecurity,
} from "../models/operations/createcollection.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * create collections
 *
 * @remarks
 * Create an MCP collection within the organization
 */
export declare function collectionsCreate(
  client: GramCore,
  request: CreateCollectionRequest,
  security?: CreateCollectionSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    MCPCollection,
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
//# sourceMappingURL=collectionsCreate.d.ts.map
