import type { InputFormat } from "./custom/definition";
import { httpsUrlProblem } from "./issuerUrl";
import type { MatchKind } from "./subjectRule";

/** A trusted platform's values, as the platform form collects them. */
export interface RegisterIssuerValues {
  name: string;
  description: string;
  issuer: string;
  jwksUri: string;
  tags: string[];
}

/** The values an access rule is allowed or edited with. */
export interface AdmitSubjectValues {
  issuer: string;
  subject: string;
  matchKind: MatchKind;
  name: string;
  tags: string[];
  agentId: string;
}

/**
 * The access form's own state. matchKind is derived and the issuer comes from
 * the page, so neither is held here.
 */
export interface AdmitSubjectFormValues {
  subject: string;
  name: string;
  tags: string[];
  agentId: string;
}

/** An allowed machine's current values, for editing it. */
export interface AdmitSubjectInitialValues extends AdmitSubjectFormValues {
  /**
   * The assigned agent's name, so the picker can show it even when the agent
   * has dropped out of the active list.
   */
  agentName: string;
}

// The column caps a name at 100 characters, as the server does.
const MAX_NAME_LENGTH = 100;

export function nameProblem(name: string): string | null {
  if (Array.from(name.trim()).length > MAX_NAME_LENGTH) {
    return `At most ${MAX_NAME_LENGTH} characters.`;
  }
  return null;
}

// The column caps a description at 500 characters, as the server does.
const MAX_DESCRIPTION_LENGTH = 500;

export function descriptionProblem(description: string): string | null {
  // Code points, as the server counts them, not UTF-16 units.
  if (Array.from(description.trim()).length > MAX_DESCRIPTION_LENGTH) {
    return `At most ${MAX_DESCRIPTION_LENGTH} characters.`;
  }
  return null;
}

/**
 * Why a value fails the validator its input's format names. A subject rule's
 * problems depend on the platform it is allowed under, so the access form
 * checks those itself and this reports none.
 */
export function formatProblem(
  format: InputFormat,
  value: string,
): string | null {
  switch (format) {
    case "issuer_url":
      return httpsUrlProblem(value, true);
    case "jwks_uri":
      return httpsUrlProblem(value, false);
    case "platform_name":
      return nameProblem(value);
    case "platform_description":
      return descriptionProblem(value);
    case "subject_rule":
    case "none":
      return null;
  }
}
