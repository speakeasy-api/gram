import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { Role } from "../models/components/role.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { GetRoleRequest, GetRoleSecurity } from "../models/operations/getrole.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * getRole access
 *
 * @remarks
 * Get a role by ID.
 */
export declare function accessGetRole(client: GramCore, request: GetRoleRequest, security?: GetRoleSecurity | undefined, options?: RequestOptions): APIPromise<Result<Role, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=accessGetRole.d.ts.map