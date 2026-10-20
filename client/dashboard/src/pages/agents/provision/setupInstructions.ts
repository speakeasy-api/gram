import type { ReactNode } from "react";
import type { ComputedValueKey } from "./workloadCatalog";

/**
 * A platform's guided setup, written as components rather than carried as
 * data.
 *
 * The policy half — issuer, keys, subject shape, the values to collect — is
 * the server's, because the server validates tokens against it. How a setup
 * is explained is the dashboard's: it is prose and layout, it changes with
 * the UI around it, and routing it through a schema and an API only meant a
 * release on both sides to fix a sentence.
 */

/**
 * Where a step sits in the shape every federation setup has: say what you are
 * federating, save it, then point the platform at Speakeasy.
 *
 * The wizard reads this rather than a step's position: `create` is where its
 * Connect button goes, and `connect` steps describe work in another product's
 * console against a rule that has to exist first.
 */
export type SetupPhase = "collect" | "create" | "connect";

/** What a step's body is given. */
export type SetupContext = {
  /** What the reader has typed so far, by field key. */
  values: Record<string, string>;
  setValue: (key: string, value: string) => void;
  /** The agent the rule will name. The wizard is making it, so it is told. */
  agentName: string;
  /** Values only the server can supply: token endpoint, issuer URL, host. */
  computed: Partial<Record<ComputedValueKey, string>>;
  /** The issuer picker, folded away unless it has something to explain. */
  issuerPicker: ReactNode;
  /** Set when the rule being saved already exists against another agent. */
  ruleConflict: string | null;
};

export type SetupStep = {
  id: string;
  title: string;
  /** One or two words, for a stepper too full to carry the title. */
  short: string;
  phase: SetupPhase;
  body: (context: SetupContext) => ReactNode;
  /** Whether the reader has given this step what it asks for. */
  complete?: (context: SetupContext) => boolean;
};

/** What saving the setup writes: a trusted platform and one access rule. */
export type Connection = {
  name: string;
  description: string;
  issuer: string;
  jwksUri: string;
  subject: string;
  matchKind: "exact" | "wildcard";
};

export type PlatformSetup = {
  /** What the reader must already have, folded away on the first step. */
  requirements?: ReactNode;
  steps: SetupStep[];
  /** The rows to write, or null while the values are incomplete. */
  connection: (values: Record<string, string>) => Connection | null;
};
