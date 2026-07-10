import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { AccessMember } from "../models/components/accessmember.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { UpdateMemberRolesRequest, UpdateMemberRolesSecurity } from "../models/operations/updatememberroles.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * updateMemberRoles access
 *
 * @remarks
 * Update a team member's role assignments.
 */
export declare function accessUpdateMemberRoles(client: GramCore, request: UpdateMemberRolesRequest, security?: UpdateMemberRolesSecurity | undefined, options?: RequestOptions): APIPromise<Result<AccessMember, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=accessUpdateMemberRoles.d.ts.map