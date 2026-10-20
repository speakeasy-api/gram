import type { WorkloadAdmission } from "@gram/client/models/components/workloadadmission.js";
import { useWorkloadIdentities } from "@gram/client/react-query/workloadIdentities.js";

/**
 * The platforms whose own tokens sign in as this agent.
 *
 * An admission names an agent, so this is a filter rather than a request for
 * a slice the server does not offer.
 */
export function useAgentAdmissions(agentID: string): {
  admissions: WorkloadAdmission[];
  isPending: boolean;
  isError: boolean;
  refetch: () => void;
} {
  // The server reports an admission with no agent assigned as `agentId: ""`,
  // so matching on an empty id would read a stray unassigned rule as this
  // agent's. The new-agent flow asks with an empty id until the agent exists.
  const known = agentID !== "";
  const identities = useWorkloadIdentities({}, undefined, { enabled: known });
  if (!known) {
    return {
      admissions: [],
      // Nothing was asked for, so nothing is in flight and nothing failed.
      isPending: false,
      isError: false,
      refetch: () => {},
    };
  }
  return {
    admissions: (identities.data?.admissions ?? []).filter(
      (admission) => admission.agentId === agentID,
    ),
    isPending: identities.isPending,
    isError: identities.isError,
    refetch: () => void identities.refetch(),
  };
}
