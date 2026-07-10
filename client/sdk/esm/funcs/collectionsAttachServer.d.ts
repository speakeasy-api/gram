import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { MCPCollection } from "../models/components/mcpcollection.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { AttachServerToCollectionRequest, AttachServerToCollectionSecurity } from "../models/operations/attachservertocollection.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * attachServer collections
 *
 * @remarks
 * Attach a server to a collection. Provide exactly one of toolset_id or mcp_server_id.
 */
export declare function collectionsAttachServer(client: GramCore, request: AttachServerToCollectionRequest, security?: AttachServerToCollectionSecurity | undefined, options?: RequestOptions): APIPromise<Result<MCPCollection, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=collectionsAttachServer.d.ts.map