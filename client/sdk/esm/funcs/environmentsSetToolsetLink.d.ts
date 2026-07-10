import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ToolsetEnvironmentLink } from "../models/components/toolsetenvironmentlink.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { SetToolsetEnvironmentLinkRequest, SetToolsetEnvironmentLinkSecurity } from "../models/operations/settoolsetenvironmentlink.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * setToolsetEnvironmentLink environments
 *
 * @remarks
 * Set (upsert) a link between a toolset and an environment
 */
export declare function environmentsSetToolsetLink(client: GramCore, request: SetToolsetEnvironmentLinkRequest, security?: SetToolsetEnvironmentLinkSecurity | undefined, options?: RequestOptions): APIPromise<Result<ToolsetEnvironmentLink, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=environmentsSetToolsetLink.d.ts.map