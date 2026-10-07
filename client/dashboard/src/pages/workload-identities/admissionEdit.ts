import type { AdmitSubjectFormValues } from "./formValues";

/** The editable fields of an allowed machine that differ from what is stored. */
export interface AdmissionChanges {
  name?: string;
  tags?: string[];
  agentId?: string;
}

function sameTags(a: string[], b: string[]): boolean {
  return a.length === b.length && a.every((tag, index) => tag === b[index]);
}

/**
 * The fields an edit changes, compared as the server stores them (the label
 * trimmed), so that an edit sends only what the operator changed and leaves the
 * rest untouched. The subject and its match kind are not editable and are never
 * included.
 */
export function changedAdmissionFields(
  initial: AdmitSubjectFormValues,
  values: AdmitSubjectFormValues,
): AdmissionChanges {
  const changes: AdmissionChanges = {};

  // Sent as an empty string to clear it: the server stores blank as none.
  const name = values.name.trim();
  if (name !== initial.name.trim()) {
    changes.name = name;
  }

  if (!sameTags(values.tags, initial.tags)) {
    changes.tags = values.tags;
  }

  if (values.agentId.length > 0 && values.agentId !== initial.agentId) {
    changes.agentId = values.agentId;
  }

  return changes;
}

export function admissionValuesDiffer(
  initial: AdmitSubjectFormValues,
  values: AdmitSubjectFormValues,
): boolean {
  return Object.keys(changedAdmissionFields(initial, values)).length > 0;
}
