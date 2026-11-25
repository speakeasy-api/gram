import type { IconName } from "@/components/ui/Icon/names";
import type { Scope } from "@gram/client/models/components/rolegrant.js";

/**
 * The resource definition contract.
 *
 * A product team describes its resource with plain data and pure accessors:
 * what a row shows, what a record shows, which actions exist, and which
 * assistant tools open which page. The kit owns every pixel. There is no
 * render override: when a resource needs something the kit does not draw, add
 * a new column, field, or section kind to the kit (reviewed centrally) so every
 * resource can use it.
 *
 * Because the definition is data, other tools read it too: Storybook builds
 * the state matrix from it, the prototype routes assistant tool calls with it,
 * and the parity script compares `actions[].sdk` against the dashboard.
 */

export type Tone =
  | "neutral"
  | "success"
  | "warning"
  | "destructive"
  | "information";

export interface Status {
  label: string;
  tone: Tone;
}

/** The square tile that identifies a thing: a logo, or a tinted initial. */
export interface Mark {
  label: string;
  iconUrl?: string;
  /** A second, smaller tile in the corner (a gateway's first source). */
  secondary?: { label: string; iconUrl?: string };
}

export interface Identity {
  mark: Mark;
  title: string;
  subtitle?: string;
}

// ---------------------------------------------------------------------------
// Values: what a column or fact can hold. The kit decides how each one looks.
// ---------------------------------------------------------------------------

export type Value =
  | { kind: "text"; text: string; mono?: boolean }
  | { kind: "number"; value: number; unit?: { one: string; many: string } }
  | { kind: "status"; status: Status }
  | { kind: "tag"; text: string; tone?: Tone }
  | { kind: "date"; date: Date }
  | { kind: "link"; text: string; href: string; external?: boolean }
  | { kind: "empty"; text: string };

// ---------------------------------------------------------------------------
// List page
// ---------------------------------------------------------------------------

type ColumnDef<Row> =
  | {
      id: string;
      header: string;
      kind: "identity";
      value: (row: Row) => Identity;
    }
  | {
      id: string;
      header: string;
      kind: "value";
      align?: "start" | "end";
      value: (row: Row) => Value;
    };

interface FilterDef<Row> {
  id: string;
  label: string;
  options: { value: string; label: string }[];
  matches: (row: Row, value: string) => boolean;
}

interface EmptyDef {
  icon: IconName;
  heading: string;
  description: string;
}

interface ListDef<Row> {
  title: string;
  description: string;
  columns: ColumnDef<Row>[];
  /** Text the search box matches against. */
  searchText: (row: Row) => string;
  filters?: FilterDef<Row>[];
  rowId: (row: Row) => string;
  /**
   * "table" (default) for comparing many things by column; "cards" for
   * browsing a few rich things, like a catalog. Cards show the identity
   * column, a corner badge and a row of facts.
   */
  layout?: "table" | "cards";
  card?: {
    badge?: (row: Row) => { text: string; tone: Tone } | undefined;
    facts?: (row: Row) => Value[];
  };
  /** Header action, by action id. */
  primaryAction?: string;
  empty: EmptyDef;
}

// ---------------------------------------------------------------------------
// Record page
// ---------------------------------------------------------------------------

interface RecordHeader {
  kicker: string;
  identity: Identity;
  status?: Status;
  description?: string;
}

type Fact<Rec> = {
  label: string;
  value: (rec: Rec) => Value;
};

/** An editable setting. Its change runs the named action with the new value. */
export type FieldDef<Rec> =
  | {
      id: string;
      label: string;
      description?: string;
      kind: "text";
      value: (rec: Rec) => string;
      action: string;
      validate?: (value: string) => string | undefined;
    }
  | {
      id: string;
      label: string;
      description?: string;
      kind: "choice";
      value: (rec: Rec) => string;
      options: (rec: Rec) => {
        value: string;
        label: string;
        description: string;
      }[];
      action: string;
    }
  | {
      id: string;
      label: string;
      description?: string;
      kind: "toggle";
      value: (rec: Rec) => boolean;
      action: string;
    };

interface ListItem {
  id: string;
  title: string;
  description?: string;
  mono?: boolean;
  /** Opens another resource's record, if it has one. */
  opens?: { resource: string; id: string };
}

interface ChecklistStep {
  id: string;
  title: string;
  description: string;
  done: boolean;
  /** Runs this action when the step is current. */
  action?: string;
  optional?: boolean;
}

interface CodeSample {
  id: string;
  label: string;
  code: string;
}

interface ActivityEntry {
  id: string;
  at: Date;
  text: string;
  tone?: Tone;
}

/** Every section kind can say when it applies to a record. */
interface SectionBase<Rec> {
  id: string;
  when?: (rec: Rec) => boolean;
}

export type SectionDef<Rec> = SectionBase<Rec> &
  (
    | { kind: "facts"; facts: Fact<Rec>[] }
    | {
        kind: "checklist";
        title: string;
        steps: (rec: Rec) => ChecklistStep[];
        /** Hide the section once every required step is done. */
        hideWhenDone?: boolean;
      }
    | {
        kind: "list";
        title: string;
        items: (rec: Rec) => ListItem[];
        /** Shown when there are more than the list holds ("Showing 6 of 24"). */
        total?: (rec: Rec) => number;
        empty: string;
        /** Header action, by action id (e.g. "Add tools"). */
        addAction?: string;
        /** Per-item action, by action id (e.g. "Remove"). Receives the item id. */
        itemAction?: string;
      }
    | {
        kind: "code";
        title: string;
        description?: string;
        samples: (rec: Rec) => CodeSample[];
      }
    | {
        kind: "settings";
        title: string;
        fields: FieldDef<Rec>[];
      }
    | {
        kind: "activity";
        title: string;
        entries: (rec: Rec) => ActivityEntry[];
      }
  );

interface RecordDef<Rec> {
  header: (rec: Rec) => RecordHeader;
  /** The single solid button, by action id. */
  primaryAction?: (rec: Rec) => string | undefined;
  secondaryActions?: string[];
  sections: SectionDef<Rec>[];
  /** Destructive actions, drawn in the danger section at the end. */
  dangerActions?: string[];
}

// ---------------------------------------------------------------------------
// Actions
// ---------------------------------------------------------------------------

type ActionKind =
  /** Runs at once. */
  | "run"
  /** Asks before it runs; for destructive or production-changing work. */
  | "confirm"
  /** Sets a value (an editable field drives it). */
  | "set"
  /** Leaves the app (Stripe, OAuth, a docs page). */
  | "external"
  /** Hands the job to the assistant with a prepared prompt. */
  | "chat";

interface ConfirmSpec {
  title: string;
  description: string;
  confirmLabel: string;
  pendingLabel: string;
}

interface ActionBase {
  id: string;
  label: string;
  /** Write scope that must be held; the kit hides the action otherwise. */
  scope?: Scope;
  /**
   * Destructive: drawn red in its confirm dialog. Placement in the danger
   * section comes from `record.dangerActions`, not from this flag.
   */
  danger?: boolean;
  /**
   * After the action settles without an error, the record page returns to
   * the list. Use it for actions that remove the record, such as delete.
   */
  leavesRecord?: boolean;
  /** Prompt the assistant receives for `kind: "chat"` actions. */
  prompt?: string;
  /**
   * The SDK hooks or calls this action uses. Read by the parity script; not
   * used at runtime.
   */
  sdk: string[];
}

/** A `confirm` action always carries the dialog it asks with. */
export type ActionDef = ActionBase &
  (
    | { kind: "confirm"; confirm: ConfirmSpec }
    | { kind: Exclude<ActionKind, "confirm">; confirm?: undefined }
  );

// ---------------------------------------------------------------------------
// Chat routing
// ---------------------------------------------------------------------------

type ToolRoute =
  | { opens: "list" }
  | {
      opens: "record";
      /** Pulls the record id out of the tool's input or output. */
      id: (input: unknown, output: unknown) => string | undefined;
    };

export interface ChatDef {
  /** Assistant tool names that open a page of this resource. */
  tools: Record<string, ToolRoute>;
  /**
   * Words that mean this resource when the user asks for it. When one reply
   * touches several pages, the one the user's words point at opens.
   */
  keywords: string[];
  suggestions: string[];
}

// ---------------------------------------------------------------------------
// The definition
// ---------------------------------------------------------------------------

export interface ResourceDefinition<Row, Rec> {
  /** Stable id: used in URLs, stories and chat routing. */
  id: string;
  noun: { one: string; many: string };
  /** The area eyebrow (Connect, Distribute, Secure, Organization). */
  area: string;
  list: ListDef<Row>;
  record: RecordDef<Rec>;
  actions: ActionDef[];
  chat: ChatDef;
}

// ---------------------------------------------------------------------------
// Data: the only thing that differs between Storybook and the live app.
// ---------------------------------------------------------------------------

export type Query<T> =
  | { status: "loading" }
  | { status: "error"; message: string; retry?: () => void }
  | { status: "ready"; data: T };

interface ActionState {
  run: (input?: string) => void;
  pending: boolean;
  /** The last run failed; a `leavesRecord` action then stays on the record. */
  failed?: boolean;
  /** Present when the user cannot run it; the kit shows the reason. */
  disabledReason?: string;
}

/** Action id → how to run it, for one target (a row, a record, or none). */
export type ActionHandlers = Partial<Record<string, ActionState>>;

export interface ResourceData<Row, Rec> {
  useList: () => Query<Row[]>;
  useRecord: (id: string) => Query<Rec>;
  /** Handlers for collection-level actions (create, import). */
  useListActions: () => ActionHandlers;
  /** Handlers for one record's actions. */
  useRecordActions: (rec: Rec | undefined) => ActionHandlers;
}

export interface ResourceModule<Row, Rec> {
  definition: ResourceDefinition<Row, Rec>;
  data: ResourceData<Row, Rec>;
}

export function defineResource<Row, Rec>(
  definition: ResourceDefinition<Row, Rec>,
): ResourceDefinition<Row, Rec> {
  return definition;
}
