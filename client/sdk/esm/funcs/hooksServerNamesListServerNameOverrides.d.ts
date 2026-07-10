import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ServerNameOverride } from "../models/components/servernameoverride.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListServerNameOverridesRequest, ListServerNameOverridesSecurity } from "../models/operations/listservernameoverrides.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * list hooksServerNames
 *
 * @remarks
 * List all server name display overrides for a project
 */
export declare function hooksServerNamesListServerNameOverrides(client: GramCore, request?: ListServerNameOverridesRequest | undefined, security?: ListServerNameOverridesSecurity | undefined, options?: RequestOptions): APIPromise<Result<Array<ServerNameOverride>, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=hooksServerNamesListServerNameOverrides.d.ts.map