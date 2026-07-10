import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { OrganizationInvitation } from "../models/components/organizationinvitation.js";
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
  SendInviteRequest,
  SendInviteSecurity,
} from "../models/operations/sendinvite.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * sendInvite organizations
 *
 * @remarks
 * Send a WorkOS invitation for the active organization.
 */
export declare function organizationsSendInvite(
  client: GramCore,
  request: SendInviteRequest,
  security?: SendInviteSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    OrganizationInvitation,
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
//# sourceMappingURL=organizationsSendInvite.d.ts.map
