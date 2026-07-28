import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListInvitesResult } from "../models/components/listinvitesresult.js";
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
  ListInvitesRequest,
  ListInvitesSecurity,
} from "../models/operations/listinvites.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * listInvites organizations
 *
 * @remarks
 * List pending WorkOS invitations for the active organization.
 */
export declare function organizationsListInvites(
  client: GramCore,
  request?: ListInvitesRequest | undefined,
  security?: ListInvitesSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    ListInvitesResult,
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
//# sourceMappingURL=organizationsListInvites.d.ts.map
