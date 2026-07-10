import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { Role } from "../models/components/role.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { UpdateRoleRequest, UpdateRoleSecurity } from "../models/operations/updaterole.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * updateRole access
 *
 * @remarks
 * Update an existing custom role.
 */
export declare function accessUpdateRole(client: GramCore, request: UpdateRoleRequest, security?: UpdateRoleSecurity | undefined, options?: RequestOptions): APIPromise<Result<Role, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=accessUpdateRole.d.ts.map