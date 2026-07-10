import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { CustomDomain } from "../models/components/customdomain.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { UpdateDomainRequest, UpdateDomainSecurity } from "../models/operations/updatedomain.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * updateDomain domains
 *
 * @remarks
 * Update the IP allowlist for the organization's custom domain
 */
export declare function domainsUpdateDomain(client: GramCore, request: UpdateDomainRequest, security?: UpdateDomainSecurity | undefined, options?: RequestOptions): APIPromise<Result<CustomDomain, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=domainsUpdateDomain.d.ts.map