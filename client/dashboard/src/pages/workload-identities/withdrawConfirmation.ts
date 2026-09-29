import type { WorkloadAdmission } from "@gram/client/models/components/workloadadmission.js";

/**
 * What the operator must type to withdraw a machine: its subject, which is what
 * the admission actually matches, rather than a label that another machine
 * could share.
 */
export function withdrawConfirmation(admission: WorkloadAdmission): string {
  return admission.subject;
}
