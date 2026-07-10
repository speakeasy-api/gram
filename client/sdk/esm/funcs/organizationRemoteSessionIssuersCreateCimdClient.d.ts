import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import * as components from "../models/components/index.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import * as errors from "../models/errors/index.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import * as operations from "../models/operations/index.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * createCimdClient organizationRemoteSessionIssuers
 *
 * @remarks
 * Register a standalone remote_session_client in Client ID Metadata Document (CIMD) mode under an existing remote_session_issuer in the caller's organization, with no user_session_issuer attachments. Gram generates the client_id and hosts the metadata document; the issuer must advertise client_id_metadata_document_supported. The client is project-scoped: it inherits a project-specific issuer's project, or the caller names a project (which must belong to the organization) when the issuer is organization-level. Requires org:admin.
 */
export declare function organizationRemoteSessionIssuersCreateCimdClient(client: GramCore, request: operations.CreateCimdOrganizationRemoteSessionClientRequest, security?: operations.CreateCimdOrganizationRemoteSessionClientSecurity | undefined, options?: RequestOptions): APIPromise<Result<components.RemoteSessionClient, errors.ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=organizationRemoteSessionIssuersCreateCimdClient.d.ts.map