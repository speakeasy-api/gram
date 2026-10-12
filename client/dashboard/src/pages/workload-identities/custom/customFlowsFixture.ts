import {
  type WorkloadCustomFlows,
  WorkloadCustomFlows$inboundSchema,
} from "@gram/client/models/components/workloadcustomflows.js";
import customFlowsResponse from "./custom.gen.json";

/**
 * The server's custom flows, exactly as `workloadIdentities.getCustomFlows`
 * returns them, for tests. `custom.gen.json` is written from the flows' YAML
 * by `mise run gen:workload-catalog-fixture`, and is parsed here the way the
 * SDK parses the response, so tests render the real forms.
 */
export const customFlows: WorkloadCustomFlows =
  WorkloadCustomFlows$inboundSchema.parse(customFlowsResponse);
