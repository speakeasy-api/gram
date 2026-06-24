// Section definitions for the policy create/edit form (AGE-2704). Kept in a
// non-component module so `wizard-chrome.tsx` exports only components (Fast
// Refresh requirement).
//
// The form renders every section at once on a single scrollable page; these
// drive the sticky section-nav rail (labels + anchor ids).

import type { FormSectionDef } from "./wizard-chrome";

/** Sections for the standard (detection-rule) policy form. The anchor ids match
 *  the `FormSection` ids in `risk-policy-body.tsx`. */
export const POLICY_FORM_SECTIONS: FormSectionDef[] = [
  { id: "detection", title: "Detect", badge: "Required" },
  { id: "scope", title: "Scope", badge: "Optional" },
  { id: "action", title: "Action", badge: "Required" },
  { id: "details", title: "Details" },
];

/** Sections for the prompt-based policy form: section 0 is the guardrail prompt
 *  (+ advanced judge config) instead of detectors. */
export const PROMPT_FORM_SECTIONS: FormSectionDef[] = [
  { id: "guardrail", title: "Guardrail", badge: "Required" },
  POLICY_FORM_SECTIONS[1]!,
  POLICY_FORM_SECTIONS[2]!,
  POLICY_FORM_SECTIONS[3]!,
];
