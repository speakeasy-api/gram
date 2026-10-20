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
  const identities = useWorkloadIdentities({});
  return {
    admissions: (identities.data?.admissions ?? []).filter(
      (admission) => admission.agentId === agentID,
    ),
    isPending: identities.isPending,
    isError: identities.isError,
    refetch: () => void identities.refetch(),
  };
}
