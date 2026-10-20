import type { GenericBlock } from "@/components/setup-steps/genericBlocks";

/**
 * The shape of a catalog platform and its guided setup.
 *
 * An entry carries what an operator cannot be expected to know about a
 * platform (its issuer, where it publishes its keys, and the shape of its
 * subjects) and what they must supply themselves, as variables. Registering
 * from an entry writes ordinary issuer and admission rows: nothing here is read
 * on the verification path.
 */

/** Whether the operator sees a value the entry supplies. */
type Visibility = "hidden" | "read-only";

interface CatalogConstant {
  value: string;
  visibility: Visibility;
}

/**
 * Something the operator supplies. A platform-tier variable is fixed once per
 * trusted platform; a rule-tier variable is supplied once per access rule.
 */
export interface CatalogVariable {
  key: string;
  tier: "platform" | "rule";
  label: string;
  help?: string;
  placeholder?: string;
  /** Anchored by the validator; a value must match it in full. */
  pattern?: string;
  patternMessage?: string;
}

/**
 * The subject an access rule matches, with `{key}` placeholders for rule-tier
 * variables. A wildcard template is a stem: the rule is the filled template
 * followed by `*`, and the template must end on a delimiter of the platform's
 * subject format so the stem cannot run into a neighboring value.
 */
interface SubjectTemplate {
  template: string;
  wildcard: boolean;
}

export interface CatalogEntry {
  /** Permanent: written onto the rows this entry creates. */
  key: string;
  displayName: string;
  description: string;
  /** The platform's logo, a path on the dashboard's origin. */
  icon?: string;
  issuer: CatalogConstant;
  jwksUri: CatalogConstant;
  variables: CatalogVariable[];
  subject: SubjectTemplate;
  /** A disabled entry stays listed so operators can see it is known. */
  enabled: boolean;
  /** Without a guided setup, the entry is listed as coming soon and cannot be opened. */
  setup?: SetupDefinition;
}

/**
 * Values the server derives for the platform to be pointed at. A definition can place
 * them but never compute them, so the console values an operator copies are the
 * ones the authorization server's metadata publishes.
 */
export type ComputedValueKey = "token_endpoint" | "issuer_url" | "mcp_host";

export type SetupBlock =
  | GenericBlock
  | { type: "field"; variable: string }
  | { type: "subject_rule" }
  | { type: "agent_picker" }
  | { type: "tags" }
  | { type: "computed_status" }
  | { type: "computed"; value: ComputedValueKey; label: string; help?: string }
  | ChecklistItemBlock;

/**
 * One thing to do in the platform's console, ticked off as the operator goes:
 * a field and the value Speakeasy derives for it, or a field or control and
 * what to do with it.
 */
export type ChecklistItemBlock = {
  type: "checklist_item";
  label: string;
  help?: string;
} & (
  | { value: ComputedValueKey; instruction?: undefined }
  | { value?: undefined; instruction: string }
);

/**
 * Steps before `create` collect what the trusted platform and its first access
 * rule need; the create step writes them; steps after it describe the
 * platform's side and only unlock once the rows exist.
 */
type SetupPhase = "collect" | "create" | "connect";

export interface SetupStep {
  id: string;
  title: string;
  phase: SetupPhase;
  blocks: SetupBlock[];
}

export interface SetupDefinition {
  steps: SetupStep[];
}
