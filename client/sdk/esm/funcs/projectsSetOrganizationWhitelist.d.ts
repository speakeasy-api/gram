import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { SetOrganizationWhitelistRequest, SetOrganizationWhitelistSecurity } from "../models/operations/setorganizationwhitelist.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * setOrganizationWhitelist projects
 *
 * @remarks
 * Set organization whitelist status (admin only - requires speakeasy-team API key)
 */
export declare function projectsSetOrganizationWhitelist(client: GramCore, request: SetOrganizationWhitelistRequest, security?: SetOrganizationWhitelistSecurity | undefined, options?: RequestOptions): APIPromise<Result<void, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=projectsSetOrganizationWhitelist.d.ts.map