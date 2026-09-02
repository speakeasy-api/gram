import { telemetryGetEmployeeDataFlowGraph } from "@gram/client/funcs/telemetryGetEmployeeDataFlowGraph";
import type { GetEmployeeDataFlowGraphResult } from "@gram/client/models/components/getemployeedataflowgraphresult.js";
import { unwrapAsync } from "@gram/client/types/fp";

export async function fetchIdentityDataFlowGraph(
  client: Parameters<typeof telemetryGetEmployeeDataFlowGraph>[0],
  from: Date,
  to: Date,
  userId: string,
  externalOrgId: string,
): Promise<GetEmployeeDataFlowGraphResult> {
  return unwrapAsync(
    telemetryGetEmployeeDataFlowGraph(client, {
      getEmployeeDataFlowGraphPayload: {
        from,
        to,
        userId,
        externalOrgId: externalOrgId || undefined,
      },
    }),
  );
}
