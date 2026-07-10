import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { MCPCollection } from "../models/components/mcpcollection.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { UpdateCollectionRequest, UpdateCollectionSecurity } from "../models/operations/updatecollection.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * update collections
 *
 * @remarks
 * Update an MCP collection
 */
export declare function collectionsUpdate(client: GramCore, request: UpdateCollectionRequest, security?: UpdateCollectionSecurity | undefined, options?: RequestOptions): APIPromise<Result<MCPCollection, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=collectionsUpdate.d.ts.map