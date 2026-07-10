import * as z from "zod/v4-mini";
import { OtelForwardingHeaderInput, OtelForwardingHeaderInput$Outbound } from "./otelforwardingheaderinput.js";
export type CreateDestinationRequestBody = {
    /**
     * Whether the destination should be active from the moment it is created.
     */
    enabled: boolean;
    /**
     * URL to forward OTEL payloads to.
     */
    endpointUrl: string;
    /**
     * Headers to attach to every forwarded request.
     */
    headers?: Array<OtelForwardingHeaderInput> | undefined;
    /**
     * Human-readable name. Unique within (org, project).
     */
    name: string;
};
/** @internal */
export type CreateDestinationRequestBody$Outbound = {
    enabled: boolean;
    endpoint_url: string;
    headers?: Array<OtelForwardingHeaderInput$Outbound> | undefined;
    name: string;
};
/** @internal */
export declare const CreateDestinationRequestBody$outboundSchema: z.ZodMiniType<CreateDestinationRequestBody$Outbound, CreateDestinationRequestBody>;
export declare function createDestinationRequestBodyToJSON(createDestinationRequestBody: CreateDestinationRequestBody): string;
//# sourceMappingURL=createdestinationrequestbody.d.ts.map