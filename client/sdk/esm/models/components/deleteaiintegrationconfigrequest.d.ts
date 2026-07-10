import * as z from "zod/v4-mini";
export type DeleteAIIntegrationConfigRequest = {
    /**
     * AI provider identifier. Supported values include cursor and anthropic_compliance.
     */
    provider: string;
};
/** @internal */
export type DeleteAIIntegrationConfigRequest$Outbound = {
    provider: string;
};
/** @internal */
export declare const DeleteAIIntegrationConfigRequest$outboundSchema: z.ZodMiniType<DeleteAIIntegrationConfigRequest$Outbound, DeleteAIIntegrationConfigRequest>;
export declare function deleteAIIntegrationConfigRequestToJSON(deleteAIIntegrationConfigRequest: DeleteAIIntegrationConfigRequest): string;
//# sourceMappingURL=deleteaiintegrationconfigrequest.d.ts.map