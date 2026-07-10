import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListMembersResult } from "../models/components/listmembersresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListMembersRequest, ListMembersSecurity } from "../models/operations/listmembers.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * listMembers access
 *
 * @remarks
 * List all team members with their role assignments.
 */
export declare function accessListMembers(client: GramCore, request?: ListMembersRequest | undefined, security?: ListMembersSecurity | undefined, options?: RequestOptions): APIPromise<Result<ListMembersResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=accessListMembers.d.ts.map