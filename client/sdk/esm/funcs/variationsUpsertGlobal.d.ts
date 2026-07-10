import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { UpsertGlobalToolVariationResult } from "../models/components/upsertglobaltoolvariationresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { UpsertGlobalVariationRequest, UpsertGlobalVariationSecurity } from "../models/operations/upsertglobalvariation.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * upsertGlobal variations
 *
 * @remarks
 * Create or update a globally defined tool variation.
 */
export declare function variationsUpsertGlobal(client: GramCore, request: UpsertGlobalVariationRequest, security?: UpsertGlobalVariationSecurity | undefined, options?: RequestOptions): APIPromise<Result<UpsertGlobalToolVariationResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=variationsUpsertGlobal.d.ts.map