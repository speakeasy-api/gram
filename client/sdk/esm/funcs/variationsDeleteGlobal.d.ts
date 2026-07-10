import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { DeleteGlobalToolVariationResult } from "../models/components/deleteglobaltoolvariationresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { DeleteGlobalVariationRequest, DeleteGlobalVariationSecurity } from "../models/operations/deleteglobalvariation.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * deleteGlobal variations
 *
 * @remarks
 * Create or update a globally defined tool variation.
 */
export declare function variationsDeleteGlobal(client: GramCore, request: DeleteGlobalVariationRequest, security?: DeleteGlobalVariationSecurity | undefined, options?: RequestOptions): APIPromise<Result<DeleteGlobalToolVariationResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=variationsDeleteGlobal.d.ts.map