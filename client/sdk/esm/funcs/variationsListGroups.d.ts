import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListToolVariationGroupsResult } from "../models/components/listtoolvariationgroupsresult.js";
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
  ListToolVariationGroupsRequest,
  ListToolVariationGroupsSecurity,
} from "../models/operations/listtoolvariationgroups.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * listGroups variations
 *
 * @remarks
 * List the tool variation groups visible to the project. In v1 this returns the project-default group when it exists, or an empty list otherwise.
 */
export declare function variationsListGroups(
  client: GramCore,
  request?: ListToolVariationGroupsRequest | undefined,
  security?: ListToolVariationGroupsSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    ListToolVariationGroupsResult,
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
//# sourceMappingURL=variationsListGroups.d.ts.map
