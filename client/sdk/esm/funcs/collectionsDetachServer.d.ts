import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { DetachServerFromCollectionRequest, DetachServerFromCollectionSecurity } from "../models/operations/detachserverfromcollection.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * detachServer collections
 *
 * @remarks
 * Detach a server from a collection. Provide exactly one of toolset_id or mcp_server_id.
 */
export declare function collectionsDetachServer(client: GramCore, request: DetachServerFromCollectionRequest, security?: DetachServerFromCollectionSecurity | undefined, options?: RequestOptions): APIPromise<Result<void, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=collectionsDetachServer.d.ts.map