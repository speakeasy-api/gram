import type { GenericBlock } from "@/components/setup-steps/genericBlocks";

/**
 * The shape of the custom flows: the forms for trusting a platform the catalog
 * does not list and allowing its workloads. Each is one step whose blocks are
 * rendered in order; what the form submits is decided by the sheet showing it.
 */

/** A value a form collects with an input. label is submitted as the name. */
export type InputField =
  | "name"
  | "description"
  | "issuer"
  | "jwks_uri"
  | "subject"
  | "label";

/** The dashboard validator an input's value must pass. */
export type InputFormat =
  | "issuer_url"
  | "jwks_uri"
  | "platform_name"
  | "platform_description"
  | "subject_rule"
  | "none";

export interface InputBlock {
  type: "input";
  field: InputField;
  format: InputFormat;
  label: string;
  placeholder?: string;
  /** Markdown shown while the value has no problem. */
  help?: string;
  multiline: boolean;
  /** Shown without letting it change; the form never submits it. */
  readOnly: boolean;
}

export interface AgentPickerBlock {
  type: "agent_picker";
  label: string;
  placeholder?: string;
  /** Markdown shown under the picker. */
  help?: string;
}

export interface TagsBlock {
  type: "tags";
  label: string;
  placeholder?: string;
  /** Markdown shown while the tags have no problem. */
  help?: string;
}

/** Where the form warns that a wildcard rule admits more than one identity. */
interface WildcardCautionBlock {
  type: "wildcard_caution";
}

export type FormBlock =
  | Extract<GenericBlock, { type: "text" | "link" }>
  | InputBlock
  | AgentPickerBlock
  | TagsBlock
  | WildcardCautionBlock;

interface FormStep {
  id: string;
  title: string;
  blocks: FormBlock[];
}

export interface CustomForm {
  title: string;
  description: string;
  submitLabel: string;
  pendingLabel: string;
  /** The server holds a custom flow to exactly one step. */
  steps: FormStep[];
}

export interface CustomFlows {
  registerPlatform: CustomForm;
  editPlatform: CustomForm;
  allowAccess: CustomForm;
  editAccess: CustomForm;
}
