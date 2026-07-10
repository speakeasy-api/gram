import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListKeysResult } from "../models/components/listkeysresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListAPIKeysRequest, ListAPIKeysSecurity } from "../models/operations/listapikeys.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * listKeys keys
 *
 * @remarks
 * List all api keys for an organization
 */
export declare function keysList(client: GramCore, request?: ListAPIKeysRequest | undefined, security?: ListAPIKeysSecurity | undefined, options?: RequestOptions): APIPromise<Result<ListKeysResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=keysList.d.ts.map