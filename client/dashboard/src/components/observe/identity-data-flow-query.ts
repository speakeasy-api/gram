import { telemetryGetEmployeeDataFlowGraph } from "@gram/client/funcs/telemetryGetEmployeeDataFlowGraph";
import type { GetEmployeeDataFlowGraphResult } from "@gram/client/models/components/getemployeedataflowgraphresult.js";
import { unwrapAsync } from "@gram/client/types/fp";

/**
 * The subject to draw the graph for. The endpoint takes exactly one of the
 * two, and they are not interchangeable: an identity the directory has never
 * heard of is keyed on the id its agent reported, and sending that as a user
 * id filters on a directory row that does not exist — an empty graph rather
 * than an error.
 */
export type IdentityDataFlowSubject =
  | { userId: string }
  | { externalUserId: string };

export async function fetchIdentityDataFlowGraph(
  client: Parameters<typeof telemetryGetEmployeeDataFlowGraph>[0],
  from: Date,
  to: Date,
  subject: IdentityDataFlowSubject,
  externalOrgId: string,
): Promise<GetEmployeeDataFlowGraphResult> {
  return unwrapAsync(
    telemetryGetEmployeeDataFlowGraph(client, {
      getEmployeeDataFlowGraphPayload: {
        from,
        to,
        ...subject,
        externalOrgId: externalOrgId || undefined,
      },
    }),
  );
}
