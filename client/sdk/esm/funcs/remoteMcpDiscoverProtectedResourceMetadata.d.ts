import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ProtectedResourceMetadataDiscovery } from "../models/components/protectedresourcemetadatadiscovery.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { DiscoverRemoteMcpProtectedResourceMetadataRequest, DiscoverRemoteMcpProtectedResourceMetadataSecurity } from "../models/operations/discoverremotemcpprotectedresourcemetadata.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * discoverProtectedResourceMetadata remoteMcp
 *
 * @remarks
 * Probe the remote MCP server's origin for an RFC 9728 .well-known/oauth-protected-resource document and return either the parsed metadata or a typed unavailability reason. Runs server-side under guardian.Policy so production resource servers without CORS can still be inspected.
 */
export declare function remoteMcpDiscoverProtectedResourceMetadata(client: GramCore, request: DiscoverRemoteMcpProtectedResourceMetadataRequest, security?: DiscoverRemoteMcpProtectedResourceMetadataSecurity | undefined, options?: RequestOptions): APIPromise<Result<ProtectedResourceMetadataDiscovery, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=remoteMcpDiscoverProtectedResourceMetadata.d.ts.map