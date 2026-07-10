import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GenerateWorkOSAdminPortalLinkResult } from "../models/components/generateworkosadminportallinkresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { GenerateWorkOSAdminPortalLinkRequest, GenerateWorkOSAdminPortalLinkSecurity } from "../models/operations/generateworkosadminportallink.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * generateWorkOSAdminPortalLink organizations
 *
 * @remarks
 * Generate a WorkOS Admin Portal link for the given intent (e.g. dsync, sso).
 */
export declare function organizationsGenerateWorkOSAdminPortalLink(client: GramCore, request: GenerateWorkOSAdminPortalLinkRequest, security?: GenerateWorkOSAdminPortalLinkSecurity | undefined, options?: RequestOptions): APIPromise<Result<GenerateWorkOSAdminPortalLinkResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=organizationsGenerateWorkOSAdminPortalLink.d.ts.map