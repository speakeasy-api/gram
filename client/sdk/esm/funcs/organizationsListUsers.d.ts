import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListUsersResult } from "../models/components/listusersresult.js";
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
  ListOrganizationUsersRequest,
  ListOrganizationUsersSecurity,
} from "../models/operations/listorganizationusers.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * listUsers organizations
 *
 * @remarks
 * List users in the active organization from Gram organization_user_relationships.
 */
export declare function organizationsListUsers(
  client: GramCore,
  request?: ListOrganizationUsersRequest | undefined,
  security?: ListOrganizationUsersSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    ListUsersResult,
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
//# sourceMappingURL=organizationsListUsers.d.ts.map
