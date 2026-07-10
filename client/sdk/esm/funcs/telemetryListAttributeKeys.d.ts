import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListAttributeKeysResult } from "../models/components/listattributekeysresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListAttributeKeysRequest, ListAttributeKeysSecurity } from "../models/operations/listattributekeys.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * listAttributeKeys telemetry
 *
 * @remarks
 * List distinct attribute keys available for filtering
 */
export declare function telemetryListAttributeKeys(client: GramCore, request: ListAttributeKeysRequest, security?: ListAttributeKeysSecurity | undefined, options?: RequestOptions): APIPromise<Result<ListAttributeKeysResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=telemetryListAttributeKeys.d.ts.map