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
  UpdateInviteRoleRequest,
  UpdateInviteRoleSecurity,
} from "../models/operations/updateinviterole.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * updateInviteRole organizations
 *
 * @remarks
 * Change the role assigned to a pending WorkOS invitation.
 */
export declare function organizationsUpdateInviteRole(
  client: GramCore,
  request: UpdateInviteRoleRequest,
  security?: UpdateInviteRoleSecurity | undefined,
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
//# sourceMappingURL=organizationsUpdateInviteRole.d.ts.map
