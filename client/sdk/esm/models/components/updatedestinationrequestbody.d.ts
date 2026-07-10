import * as z from "zod/v4-mini";
import { OtelForwardingHeaderInput, OtelForwardingHeaderInput$Outbound } from "./otelforwardingheaderinput.js";
export type UpdateDestinationRequestBody = {
    /**
     * Updated enabled state.
     */
    enabled: boolean;
    /**
     * Updated URL.
     */
    endpointUrl: string;
    /**
     * Full set of headers to attach. Replaces any existing headers.
     */
    headers?: Array<OtelForwardingHeaderInput> | undefined;
    /**
     * The destination ID.
     */
    id: string;
    /**
     * Updated name.
     */
    name: string;
};
/** @internal */
export type UpdateDestinationRequestBody$Outbound = {
    enabled: boolean;
    endpoint_url: string;
    headers?: Array<OtelForwardingHeaderInput$Outbound> | undefined;
    id: string;
    name: string;
};
/** @internal */
export declare const UpdateDestinationRequestBody$outboundSchema: z.ZodMiniType<UpdateDestinationRequestBody$Outbound, UpdateDestinationRequestBody>;
export declare function updateDestinationRequestBodyToJSON(updateDestinationRequestBody: UpdateDestinationRequestBody): string;
//# sourceMappingURL=updatedestinationrequestbody.d.ts.map