import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { DeleteSourceEnvironmentLinkRequest, DeleteSourceEnvironmentLinkSecurity } from "../models/operations/deletesourceenvironmentlink.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * deleteSourceEnvironmentLink environments
 *
 * @remarks
 * Delete a link between a source and an environment
 */
export declare function environmentsDeleteSourceLink(client: GramCore, request: DeleteSourceEnvironmentLinkRequest, security?: DeleteSourceEnvironmentLinkSecurity | undefined, options?: RequestOptions): APIPromise<Result<void, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=environmentsDeleteSourceLink.d.ts.map