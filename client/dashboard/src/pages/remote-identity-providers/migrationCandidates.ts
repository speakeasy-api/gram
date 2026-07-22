import type { OrganizationRemoteSessionIssuer } from "@gram/client/models/components/organizationremotesessionissuer.js";

// migrationCandidates narrows the org's issuers to those a source may be
// consolidated onto, mirroring the server's scope ladder so the picker never
// offers a target the mutation would reject:
//
//   - never itself
//   - project-specific sources may target their own project or any
//     organizational issuer
//   - organizational sources may target only other organizational issuers
//
// Endpoint parity is deliberately NOT filtered here. A same-URL check would
// silently hide a near-miss target, leaving the admin with an empty picker and
// no explanation; instead every scope-legal target is offered and the preflight
// spells out exactly which fields disagree.
export function migrationCandidates(
  source: OrganizationRemoteSessionIssuer,
  all: OrganizationRemoteSessionIssuer[],
): OrganizationRemoteSessionIssuer[] {
  const sourceProjectId = source.issuer.projectId;

  return all.filter((candidate) => {
    if (candidate.issuer.id === source.issuer.id) return false;

    const candidateProjectId = candidate.issuer.projectId;
    if (!candidateProjectId) return true;

    return !!sourceProjectId && candidateProjectId === sourceProjectId;
  });
}
