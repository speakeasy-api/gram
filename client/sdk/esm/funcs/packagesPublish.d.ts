import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { PublishPackageResult } from "../models/components/publishpackageresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { PublishRequest, PublishSecurity } from "../models/operations/publish.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * publish packages
 *
 * @remarks
 * Publish a new version of a package.
 */
export declare function packagesPublish(client: GramCore, request: PublishRequest, security?: PublishSecurity | undefined, options?: RequestOptions): APIPromise<Result<PublishPackageResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=packagesPublish.d.ts.map