import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { CustomDomain } from "../models/components/customdomain.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { GetDomainRequest, GetDomainSecurity } from "../models/operations/getdomain.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * getDomain domains
 *
 * @remarks
 * Get the custom domain for an organization
 */
export declare function domainsGetDomain(client: GramCore, request?: GetDomainRequest | undefined, security?: GetDomainSecurity | undefined, options?: RequestOptions): APIPromise<Result<CustomDomain, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=domainsGetDomain.d.ts.map