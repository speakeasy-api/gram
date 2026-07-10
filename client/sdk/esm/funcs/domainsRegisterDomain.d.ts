import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { RegisterDomainRequest, RegisterDomainSecurity } from "../models/operations/registerdomain.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * createDomain domains
 *
 * @remarks
 * Create a custom domain for an organization
 */
export declare function domainsRegisterDomain(client: GramCore, request: RegisterDomainRequest, security?: RegisterDomainSecurity | undefined, options?: RequestOptions): APIPromise<Result<void, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=domainsRegisterDomain.d.ts.map