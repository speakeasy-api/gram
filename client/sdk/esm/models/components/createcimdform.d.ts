import * as z from "zod/v4-mini";
/**
 * Form for creating a remote_session_client in Client ID Metadata Document (CIMD) mode. Gram generates the client_id (the URL of a hosted client metadata document) and serves the document publicly; the row carries no secret and authenticates with token_endpoint_auth_method=none. The caller supplies no client_id or credentials.
 */
export type CreateCimdForm = {
    /**
     * Optional upstream OAuth audience to send on the authorize redirect and token exchange.
     */
    audience?: string | undefined;
    /**
     * The owning remote_session_issuer id. Must advertise client_id_metadata_document_supported.
     */
    remoteSessionIssuerId: string;
    /**
     * Explicit upstream OAuth scopes the dance should request for this client. Omit to fall back to the issuer's scopes_supported.
     */
    scope?: Array<string> | undefined;
    /**
     * The user_session_issuers to attach this client to via the join table. Omit or pass an empty array to create a standalone client with no attachments.
     */
    userSessionIssuerIds?: Array<string> | undefined;
};
/** @internal */
export type CreateCimdForm$Outbound = {
    audience?: string | undefined;
    remote_session_issuer_id: string;
    scope?: Array<string> | undefined;
    user_session_issuer_ids?: Array<string> | undefined;
};
/** @internal */
export declare const CreateCimdForm$outboundSchema: z.ZodMiniType<CreateCimdForm$Outbound, CreateCimdForm>;
export declare function createCimdFormToJSON(createCimdForm: CreateCimdForm): string;
//# sourceMappingURL=createcimdform.d.ts.map