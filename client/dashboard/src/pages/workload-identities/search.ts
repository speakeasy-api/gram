import type { WorkloadAdmission } from "@gram/client/models/components/workloadadmission.js";
import type { WorkloadIssuer } from "@gram/client/models/components/workloadissuer.js";

function matchesAny(fields: string[], query: string): boolean {
  const needle = query.trim().toLowerCase();
  if (needle === "") {
    return true;
  }
  return fields.some((field) => field.toLowerCase().includes(needle));
}

// Matches whatever an operator remembers about a platform: part of its name or
// description, its issuer URL, or a tag. Case-insensitive, and an empty query
// matches everything.
export function issuerMatches(issuer: WorkloadIssuer, query: string): boolean {
  return matchesAny(
    [issuer.name, issuer.description, issuer.issuer, ...issuer.tags],
    query,
  );
}

// The same match over what an operator is likely to remember about a machine.
export function admissionMatches(
  admission: WorkloadAdmission,
  query: string,
): boolean {
  return matchesAny(
    [admission.subject, admission.name, admission.agentName, ...admission.tags],
    query,
  );
}
