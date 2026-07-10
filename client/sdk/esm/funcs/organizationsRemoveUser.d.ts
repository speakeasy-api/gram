import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { RemoveOrganizationUserRequest, RemoveOrganizationUserSecurity } from "../models/operations/removeorganizationuser.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * removeUser organizations
 *
 * @remarks
 * Remove a user from the active organization in Gram and delete their WorkOS organization membership.
 */
export declare function organizationsRemoveUser(client: GramCore, request: RemoveOrganizationUserRequest, security?: RemoveOrganizationUserSecurity | undefined, options?: RequestOptions): APIPromise<Result<void, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=organizationsRemoveUser.d.ts.map