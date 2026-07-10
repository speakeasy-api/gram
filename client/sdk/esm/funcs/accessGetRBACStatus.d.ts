import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RBACStatus } from "../models/components/rbacstatus.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { GetRBACStatusRequest, GetRBACStatusSecurity } from "../models/operations/getrbacstatus.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * getRBACStatus access
 *
 * @remarks
 * Returns whether RBAC is currently enabled for the current organization.
 */
export declare function accessGetRBACStatus(client: GramCore, request?: GetRBACStatusRequest | undefined, security?: GetRBACStatusSecurity | undefined, options?: RequestOptions): APIPromise<Result<RBACStatus, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=accessGetRBACStatus.d.ts.map