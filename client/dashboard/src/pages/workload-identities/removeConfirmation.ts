import type { WorkloadAdmission } from "@gram/client/models/components/workloadadmission.js";

/**
 * What the operator must type to remove a machine's access: its subject, which is what
 * the admission actually matches, rather than a label that another machine
 * could share.
 */
export function removeConfirmation(admission: WorkloadAdmission): string {
  return admission.subject;
}
