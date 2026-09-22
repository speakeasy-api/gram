import {
  authorityModeLabel,
  USAGE_QUESTION,
} from "@/components/mcp-approvals/evidence";
import { Badge } from "@/components/ui/Badge";
import { Heading } from "@/components/ui/Heading";
import { SearchBar } from "@/components/ui/SearchBar";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import { Info } from "lucide-react";
import { useDeferredValue, useId, useMemo, useState } from "react";
import { HumanizeDateTime } from "@/lib/dates";
import {
  MoreToggle,
  useCollapsedPreview,
} from "@/components/ui/collapsible-preview";
import { cn } from "@/lib/utils";
import { evidenceSectionAnchor, type EvidenceSectionId } from "./signals";
import type {
  EvidenceAdvisories,
  EvidenceAdvisoryItem,
  EvidenceAuthority,
  EvidenceCapability,
  EvidenceDocument,
  EvidenceDomain,
  EvidenceExposure,
  EvidenceIdentity,
  EvidencePackage,
  EvidenceProvenance,
  EvidenceRepository,
} from "./evidence";

/**
 * The evidence panel, grouped by the question the admin is asking rather than
 * by where the data came from.
 *
 * Two rules shape everything here. Unknown must read as unknown: a group with
 * no gathered data renders a conspicuously empty block, never a clean or
 * reassuring state. And every finding worth acting on is raised once, by the
 * signals panel above — these groups show the facts behind it rather than
 * repeating the alarm beside them, which is what turned the page into a grid
 * of small bordered asides nobody could rank.
 *
 * The row order is fixed rather than derived from what was gathered. Identity
 * and authority are short paired lists, so they share the first row; the tool
 * declarations and the traffic table are both long and both need the width,
 * so they each take one. Maturity pairs with advisories only when there is a
 * package to have advisories about.
 */
export function EvidencePanel({
  document,
  usage,
}: {
  document: EvidenceDocument | null;
  /**
   * Who is calling the server today, supplied by the page that has the
   * traffic query. It reads as one more question about the server, and it
   * belongs beside the tools it declares: what it can do, and who is doing
   * it. Absent for a decision's frozen snapshot, which is evidence as it
   * stood rather than traffic as it is now.
   */
  usage?: React.ReactNode;
}): JSX.Element {
  if (!document) {
    return (
      <UnknownBlock>
        No evidence gathered. Nothing is known — which is not the same as
        nothing being wrong.
      </UnknownBlock>
    );
  }

  // Advisory databases index published packages. For anything else the group
  // could only ever say "nothing here can answer this", which it did on every
  // remote server's page — so the question moves to the signals list as an
  // unknown and stops taking half a row.
  const answerableAdvisories = document.identity.kind === "package";

  return (
    <div className="space-y-3">
      <div className="grid gap-x-6 gap-y-3 lg:grid-cols-2">
        <TrustSection
          identity={document.identity}
          pkg={document.package}
          domain={document.domain}
        />
        <AuthoritySection authority={document.authority} />
      </div>
      <div
        className={cn(
          "grid gap-x-6 gap-y-3",
          // Paired only when there is an advisory answer to pair with.
          // Otherwise the row held one box and an empty column beside it.
          answerableAdvisories && "lg:grid-cols-2",
        )}
      >
        <MaturitySection
          pkg={document.package}
          notPublished={document.packageNotPublished}
          packageName={document.identity.packageName}
          provenance={document.provenance}
          identityKind={document.identity.kind}
          repository={document.repository}
          repositoryNotFound={document.repositoryNotFound}
        />
        {answerableAdvisories && (
          <AdvisoriesSection advisories={document.advisories} />
        )}
      </div>
      <DeclaredCapabilitySection
        capabilities={document.capabilities}
        source={document.capabilitiesSource}
      />
      {usage ? (
        // The traffic table answers "are we already exposed?" on its own —
        // names, counts and recency — so that question has no group of its own
        // wherever the table is rendered.
        <EvidenceGroup question={USAGE_QUESTION} section="usage">
          {usage}
        </EvidenceGroup>
      ) : (
        // Without a traffic table the exposure figures have nowhere else to
        // appear: the review sheet has no summary strip, and a frozen decision
        // snapshot is evidence as it stood, not traffic as it is now.
        <ExposureSection
          exposure={document.exposure}
          identity={document.identity}
        />
      )}
    </div>
  );
}

/**
 * What this project's own traffic says, for the surfaces that do not render a
 * live traffic table beside it.
 */
function ExposureSection({
  exposure,
  identity,
}: {
  exposure: EvidenceExposure | undefined;
  identity: EvidenceIdentity;
}): JSX.Element {
  if (!exposure) {
    return (
      <EvidenceGroup question={USAGE_QUESTION} section="usage">
        <UnknownBlock>
          {identity.kind === "remote"
            ? "Usage records could not be gathered."
            : "No URL to look up in usage records — exposure here is unknowable from traffic."}
        </UnknownBlock>
      </EvidenceGroup>
    );
  }

  if (exposure.status === "unseen") {
    return (
      <EvidenceGroup question={USAGE_QUESTION} section="usage">
        <AnswerBlock>
          No one in this project has recorded traffic to this server.
        </AnswerBlock>
      </EvidenceGroup>
    );
  }

  const facts: Array<{ label: string; value: React.ReactNode }> = [
    { label: "People who have called it", value: exposure.userCount ?? 0 },
    { label: "Recorded calls", value: exposure.callCount ?? 0 },
  ];
  if (exposure.firstSeen) {
    facts.push({
      label: "First seen here",
      value: (
        <HumanizeDateTime
          date={new Date(exposure.firstSeen)}
          includeTime={false}
        />
      ),
    });
  }
  if (exposure.lastCalled) {
    facts.push({
      label: "Last called",
      value: <HumanizeDateTime date={new Date(exposure.lastCalled)} />,
    });
  }
  if (exposure.serverName) {
    // The name this project knew the server by when the evidence was taken —
    // on a frozen snapshot nothing else carries it, and it can differ from
    // the name the page shows today.
    facts.push({ label: "Known here as", value: exposure.serverName });
  }

  return (
    <EvidenceGroup question={USAGE_QUESTION} section="usage">
      <FactList facts={facts} />
    </EvidenceGroup>
  );
}

export function EvidenceGroup({
  question,
  note,
  hint,
  section,
  children,
}: {
  question: string;
  /**
   * The caveat about where this group's data came from and what it does not
   * prove, behind an icon beside the question. It reads as a footnote, not a
   * finding, and as a paragraph under the heading it pushed this group's box
   * down while the group beside it started at the heading — so no two columns
   * lined up. In the tooltip the boxes share one top edge across the grid.
   */
  hint?: string;
  /**
   * The group's one-line headline, set beside the question rather than in a
   * band below it. For the finding a reader should not be able to miss —
   * everything else belongs in the body.
   */
  note?: React.ReactNode;
  /**
   * Which question this is, so the signals above can link straight to it.
   * Omitted by groups nothing links to, such as a frozen snapshot's.
   */
  section?: EvidenceSectionId;
  children: React.ReactNode;
}): JSX.Element {
  // A container, so the fact lists inside decide their own column count from
  // the width this group actually got — two when it spans the page, one when
  // it is sharing a row — rather than from the viewport.
  //
  // Always full height, never just when a caller asks: the grid stretches the
  // section to its row, so anything less left a short answer's box floating
  // against a tall table beside it.
  return (
    <section
      id={section ? evidenceSectionAnchor(section) : undefined}
      // Jumping from a signal must not land the heading under the sticky page
      // header, which is what an unqualified anchor does.
      className="@container flex h-full scroll-mt-20 flex-col gap-1.5"
    >
      {/* The questions are the page's real structure, so they keep the serif
          treatment content subsections use — sized down so a full gather fits
          on one screen without zooming. */}
      <div className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1">
        <div className="flex items-center gap-1.5">
          <Heading variant="h3" className="text-lg font-thin">
            {question}
          </Heading>
          {hint && (
            <SimpleTooltip tooltip={hint}>
              {/* A button, not the bare icon: an SVG cannot take focus, and
                  the tooltip opens on focus as well as hover, so this is what
                  makes the caveat reachable by keyboard at all. */}
              <button
                type="button"
                aria-label="About this data"
                className="text-muted-foreground hover:text-foreground inline-flex shrink-0"
              >
                <Info className="size-3.5" />
              </button>
            </SimpleTooltip>
          )}
        </div>
        {note}
      </div>
      {/* The last block grows into whatever height the row settled on, so the
          short answer and the long table in one row end on the same line
          instead of one box stopping halfway up its column. */}
      <div className="flex min-h-0 flex-1 flex-col gap-1.5 [&>*:last-child]:flex-1">
        {children}
      </div>
    </section>
  );
}

/**
 * A group whose whole answer is one sentence, framed in a hairline box with
 * the sentence centred. Centred because these blocks stretch to their row's
 * height: against a tall table beside them, a line pinned to the top-left
 * corner reads as content that failed to load rather than as the answer.
 */
function AnswerBlock({
  children,
  unknown = false,
}: {
  children: React.ReactNode;
  /**
   * Draw it dashed and muted — the panel's one visual for "we could not find
   * out", deliberately distinct from a definite answer and never green.
   */
  unknown?: boolean;
}): JSX.Element {
  return (
    <div
      className={cn(
        "border-border flex items-center justify-center border px-2.5 py-1.5 text-center text-xs",
        unknown && "text-muted-foreground border-dashed",
      )}
    >
      {children}
    </div>
  );
}

function UnknownBlock({
  children,
}: {
  children: React.ReactNode;
}): JSX.Element {
  return <AnswerBlock unknown>{children}</AnswerBlock>;
}

function FactList({
  facts,
  bare = false,
}: {
  facts: Array<{ label: string; value: React.ReactNode }>;
  /** Drop the outer frame when the list nests inside an existing card. */
  bare?: boolean;
}): JSX.Element {
  return (
    <dl
      className={cn(
        "@2xl:grid-cols-2 grid grid-cols-1 gap-x-6 gap-y-1 px-3 py-2",
        !bare && "border-border border",
      )}
    >
      {facts.map((fact) => (
        <div
          key={fact.label}
          className="flex items-center justify-between gap-3"
        >
          <dt className="text-muted-foreground text-xs">{fact.label}</dt>
          {/* Mono chip rather than Badge: Badge uppercases, and casing is
              meaningful in hosts, artifact refs, and package names. */}
          {/* break-words, not break-all: prose values and dates wrap at
              spaces; only genuinely unbreakable strings (artifact refs,
              package names) split mid-token. */}
          <dd className="border-border max-w-full min-w-0 border px-1.5 py-px text-right font-mono text-xs break-words">
            {fact.value}
          </dd>
        </div>
      ))}
    </dl>
  );
}

/**
 * npm's registry idioms for "not an SPDX identifier" read as gibberish out of
 * context — "SEE LICENSE IN LICENSE" means a custom license lives in the
 * package's LICENSE file.
 */
function licenseLabel(raw: string): string {
  if (/^SEE LICENSE/i.test(raw)) return "Custom (in the package)";
  if (raw === "UNLICENSED") return "Unlicensed — not for reuse";
  return raw;
}

function identityKindLabel(identity: EvidenceIdentity): string {
  switch (identity.kind) {
    case "remote":
      return "Remote HTTP endpoint";
    case "package":
      return "Published package run locally";
    case "unresolved":
      return "Could not be identified";
  }
}

function TrustSection({
  identity,
  pkg,
  domain,
}: {
  identity: EvidenceIdentity;
  pkg: EvidencePackage | undefined;
  domain: EvidenceDomain | undefined;
}): JSX.Element {
  if (identity.kind === "unresolved") {
    return (
      <EvidenceGroup question="Who am I trusting?" section="trust">
        <UnknownBlock>
          Could not be resolved to any identifiable server. Who publishes or
          operates it is unknown.
        </UnknownBlock>
      </EvidenceGroup>
    );
  }

  const facts: Array<{ label: string; value: React.ReactNode }> = [
    { label: "Reference type", value: identityKindLabel(identity) },
  ];
  if (identity.artifactRef) {
    facts.push({ label: "Artifact", value: identity.artifactRef });
  }
  if (identity.host) {
    facts.push({ label: "Host", value: identity.host });
  }
  if (identity.registrableDomain) {
    facts.push({
      label: "Owning domain",
      value: identity.registrableDomain,
    });
  } else if (identity.kind === "remote") {
    facts.push({ label: "Owning domain", value: <Absent>none</Absent> });
  }
  if (identity.packageName) {
    facts.push({ label: "Package", value: identity.packageName });
  }
  if (pkg?.license) {
    facts.push({ label: "Declared license", value: licenseLabel(pkg.license) });
  }
  if (pkg?.maintainerCount !== undefined) {
    facts.push({ label: "Registry maintainers", value: pkg.maintainerCount });
  }
  if (domain?.registeredAt) {
    facts.push({
      label: "Domain registered",
      value: (
        <HumanizeDateTime
          date={new Date(domain.registeredAt)}
          includeTime={false}
        />
      ),
    });
  } else if (domain?.unregistered) {
    // Stated as the registry's answer to the same question the age would have
    // answered, rather than as a banner under the list. The signal above ranks
    // it; here it belongs in the row a reader is already looking for.
    facts.push({
      label: "Domain registered",
      value: <Absent>no registration on file</Absent>,
    });
  }
  if (domain?.registrar) {
    facts.push({ label: "Registrar", value: domain.registrar });
  }

  return (
    <EvidenceGroup question="Who am I trusting?" section="trust">
      <FactList facts={facts} />
    </EvidenceGroup>
  );
}

/**
 * A fact whose answer is an absence: the lookup ran and came back with
 * nothing. Set apart from a gathered value so it cannot be skimmed as one,
 * and never colored — which of these matters is the signals panel's call.
 */
function Absent({ children }: { children: React.ReactNode }): JSX.Element {
  return <span className="text-muted-foreground italic">{children}</span>;
}

function AuthoritySection({
  authority,
}: {
  authority: EvidenceAuthority | undefined;
}): JSX.Element {
  if (!authority || authority.undeclared) {
    return (
      <EvidenceGroup
        question="What is it asking me to hand over?"
        section="handover"
      >
        <UnknownBlock>
          Not exposed by the server. Unknown — not "requires nothing".
        </UnknownBlock>
      </EvidenceGroup>
    );
  }

  const facts: Array<{ label: string; value: React.ReactNode }> = [
    { label: "Auth mode", value: authorityModeLabel(authority.mode) },
  ];
  if (authority.transport) {
    facts.push({ label: "Transport", value: authority.transport });
  }
  if (authority.dynamicRegistration) {
    facts.push({ label: "Dynamic client registration", value: "published" });
  }

  return (
    <EvidenceGroup
      question="What is it asking me to hand over?"
      section="handover"
      hint="Read off the server's own authorization metadata. Scopes are the only item here the authorization server enforces; everything else is a declaration."
    >
      <div className="border-border border">
        <FactList facts={facts} bare />
        {authority.scopes.length > 0 && (
          <div className="border-border border-t px-3 py-2">
            <p className="text-eyebrow mb-1.5">Scopes it will request</p>
            <ScopeChips scopes={authority.scopes} />
          </div>
        )}
        {authority.demandedSecrets.length > 0 && (
          <SecretList
            label="Secrets it requires"
            secrets={authority.demandedSecrets}
          />
        )}
        {authority.optionalSecrets.length > 0 && (
          <SecretList
            label="Secrets it accepts"
            secrets={authority.optionalSecrets}
          />
        )}
      </div>
    </EvidenceGroup>
  );
}

/**
 * Credentials the server names, with whatever it says each one is for. Inside
 * the authority frame beside the scopes rather than as banners above it: what
 * a reviewer compares is the whole ask, and the signals panel is what says
 * which part of it is alarming.
 */
function SecretList({
  label,
  secrets,
}: {
  label: string;
  secrets: Array<{ name: string; description?: string }>;
}): JSX.Element {
  return (
    <div className="border-border border-t px-3 py-2">
      <p className="text-eyebrow mb-1.5">{label}</p>
      <ul className="space-y-1">
        {secrets.map((secret) => (
          <li key={secret.name} className="text-xs">
            <span className="border-border border px-1.5 py-px font-mono">
              {secret.name}
            </span>
            {secret.description && (
              <span className="text-muted-foreground">
                {" "}
                {secret.description}
              </span>
            )}
          </li>
        ))}
      </ul>
    </div>
  );
}

/** How many scope chips show before the rest collapses behind the toggle. */
const SCOPE_PREVIEW_COUNT = 4;

/**
 * The wrap of scope chips, with the tail collapsed behind a "+N more" toggle
 * chip once the list exceeds the preview count.
 */
function ScopeChips({ scopes }: { scopes: string[] }): JSX.Element {
  const { collapsible, expanded, toggle, visible } = useCollapsedPreview(
    scopes,
    SCOPE_PREVIEW_COUNT,
  );
  const listId = useId();

  return (
    <div id={listId} className="flex flex-wrap gap-1">
      {visible.map((scope) => (
        <span
          key={scope}
          className="border-border border px-1.5 py-px font-mono text-xs"
        >
          {scope}
        </span>
      ))}
      {collapsible && (
        <MoreToggle
          expanded={expanded}
          onToggle={toggle}
          collapsedLabel={`+${scopes.length - SCOPE_PREVIEW_COUNT} more`}
          controlId={listId}
          className="px-1.5 py-px"
        />
      )}
    </div>
  );
}

function capabilityLabel(value: string): string {
  switch (value) {
    case "destructive":
      return "declares destructive";
    case "open_world":
      return "declares open world";
    case "arbitrary_command":
      return "schema takes a command";
    case "filesystem_path":
      return "schema takes a path";
    case "arbitrary_url":
      return "schema takes a URL";
    case "credential_input":
      return "schema takes a credential";
    default:
      return value;
  }
}

function capabilitySourceNote(
  source: "server" | "registry" | undefined,
): string {
  if (source === "registry") {
    return "The registry catalog's copy — the server itself did not answer without credentials. Declarations, not limits.";
  }
  return "The server's declarations about itself — what it asks for, not what it is limited to.";
}

function DeclaredCapabilitySection({
  capabilities,
  source,
}: {
  capabilities: EvidenceCapability[];
  source: "server" | "registry" | undefined;
}): JSX.Element {
  if (capabilities.length === 0) {
    // A source that answered with zero tools is a real declaration —
    // rendered as such, never as a failed gather.
    if (source) {
      return (
        <EvidenceGroup
          question="What does it say it can do?"
          section="capabilities"
        >
          <AnswerBlock>
            {source === "registry"
              ? "The registry catalog's copy declares no tools."
              : "The server answered the listing with zero tools."}{" "}
            The listing succeeded — this is a declared-empty toolset, not a
            failed check.
          </AnswerBlock>
        </EvidenceGroup>
      );
    }
    return (
      <EvidenceGroup
        question="What does it say it can do?"
        section="capabilities"
      >
        <UnknownBlock>
          No tool declarations gathered. Silence is not harmlessness.
        </UnknownBlock>
      </EvidenceGroup>
    );
  }

  return (
    <EvidenceGroup
      question="What does it say it can do?"
      section="capabilities"
      hint={capabilitySourceNote(source)}
    >
      <ToolDeclarations capabilities={capabilities} />
    </EvidenceGroup>
  );
}

/** How many tool rows show before the rest collapses behind the toggle. */
const TOOL_PREVIEW_COUNT = 3;

/**
 * A bordered, hairline-divided list whose tail collapses behind a
 * "Show all N {noun}" toggle once it exceeds the preview count.
 */
function CollapsibleList<T>({
  items,
  itemKey,
  renderItem,
  itemClassName,
  noun,
  previewCount = TOOL_PREVIEW_COUNT,
}: {
  items: T[];
  itemKey: (item: T) => string;
  renderItem: (item: T) => React.ReactNode;
  itemClassName: string;
  /** Plural label for the toggle, e.g. "tools". */
  noun: string;
  previewCount?: number;
}): JSX.Element {
  const { collapsible, expanded, toggle, visible } = useCollapsedPreview(
    items,
    previewCount,
  );
  const listId = useId();

  return (
    <div className="border-border border">
      <ul id={listId} className="divide-border divide-y">
        {visible.map((item) => (
          <li key={itemKey(item)} className={itemClassName}>
            {renderItem(item)}
          </li>
        ))}
      </ul>
      {collapsible && (
        <MoreToggle
          expanded={expanded}
          onToggle={toggle}
          collapsedLabel={`Show all ${items.length} ${noun}`}
          controlId={listId}
          className="border-border w-full justify-center border-t px-3 py-1"
        />
      )}
    </div>
  );
}

// content-visibility: a declared tool list is whatever an untrusted server
// answered tools/list with, so its length is not ours to bound — and a
// security review must not hide rows from the person reading it. Skipping
// layout and paint for offscreen rows makes the cost proportional to what is
// on screen instead: measured here, 5000 rows go from 79ms to 13ms. The
// intrinsic size is a first-paint estimate of one row; the browser replaces
// it with the real height once a row has been rendered, so the scrollbar
// settles as the list is scrolled.
const TOOL_ROW_CLASS =
  "flex flex-wrap items-center justify-between gap-2 px-3 py-1 text-xs [contain-intrinsic-size:auto_30px] [content-visibility:auto]";

function ToolRow({ tool }: { tool: EvidenceCapability }): JSX.Element {
  return (
    <>
      <span className="font-mono">{tool.tool}</span>
      {tool.unannotated ? (
        <span className="text-muted-foreground italic">
          declares nothing — authority unknown
        </span>
      ) : (
        <span className="flex flex-wrap justify-end gap-1">
          {[...tool.declared, ...tool.schemaImplied].map((value) => (
            <span
              key={value}
              className="border-border text-muted-foreground border px-1.5 py-px"
            >
              {capabilityLabel(value)}
            </span>
          ))}
        </span>
      )}
    </>
  );
}

/** The ways a reviewer narrows a long tool listing. */
type ToolFilter = "all" | "acts" | "destructive" | "unannotated";

/** Capability values the panel treats as "this tool reaches outside itself". */
const REACHING_CAPABILITIES = [
  "arbitrary_command",
  "arbitrary_url",
  "filesystem_path",
  "credential_input",
  "open_world",
];

function toolCapabilities(tool: EvidenceCapability): string[] {
  return [...tool.declared, ...tool.schemaImplied];
}

function matchesFilter(tool: EvidenceCapability, filter: ToolFilter): boolean {
  switch (filter) {
    case "all":
      return true;
    case "acts":
      return tool.actsOnBehalf;
    case "destructive":
      return toolCapabilities(tool).includes("destructive");
    case "unannotated":
      return tool.unannotated;
  }
}

/**
 * How alarming a tool's declarations are, for the default ordering. A review
 * reads top-down and a server can declare sixty tools, so the ones that
 * declare the most authority have to be the ones on screen first.
 */
function toolWeight(tool: EvidenceCapability): number {
  const capabilities = toolCapabilities(tool);
  let weight = 0;
  if (capabilities.includes("destructive")) weight += 8;
  if (tool.actsOnBehalf) weight += 4;
  weight += capabilities.filter((value) =>
    REACHING_CAPABILITIES.includes(value),
  ).length;
  return weight;
}

/**
 * Every declared tool, ordered by how much authority it declares and
 * narrowable by name or by kind.
 *
 * The list was previously a flat alphabetical run of every tool the server
 * answered with, in a box sized to whatever group shared its row — so a
 * sixty-tool server showed six of them, in an order that put the file-deleting
 * one wherever the alphabet left it. A review has to be able to find those.
 *
 * Nothing is ever hidden without saying so: the filter row carries the counts,
 * and a filter that matches nothing says the tools exist and this view
 * excluded them.
 */
function ToolDeclarations({
  capabilities,
}: {
  capabilities: EvidenceCapability[];
}): JSX.Element {
  const [query, setQuery] = useState("");
  const [filter, setFilter] = useState<ToolFilter>("all");
  const deferredQuery = useDeferredValue(query);

  const counts = useMemo(
    () => ({
      all: capabilities.length,
      acts: capabilities.filter((tool) => matchesFilter(tool, "acts")).length,
      destructive: capabilities.filter((tool) =>
        matchesFilter(tool, "destructive"),
      ).length,
      unannotated: capabilities.filter((tool) =>
        matchesFilter(tool, "unannotated"),
      ).length,
    }),
    [capabilities],
  );

  const ordered = useMemo(() => {
    const needle = deferredQuery.trim().toLowerCase();
    return capabilities
      .filter(
        (tool) =>
          matchesFilter(tool, filter) &&
          (needle === "" || tool.tool.toLowerCase().includes(needle)),
      )
      .sort((left, right) => {
        const byWeight = toolWeight(right) - toolWeight(left);
        if (byWeight !== 0) return byWeight;
        return left.tool.localeCompare(right.tool);
      });
  }, [capabilities, deferredQuery, filter]);

  // Only offer a filter that would select something. A row of segments where
  // three of the four are empty is a worse read than a single "All".
  const options = (
    [
      { value: "all", label: `All ${counts.all}` },
      {
        value: "acts",
        label: `Acts for you ${counts.acts}`,
        tooltip: "Tools that declare they do more than read.",
      },
      {
        value: "destructive",
        label: `Destructive ${counts.destructive}`,
        tooltip: "Tools the server annotates as not reversible by it.",
      },
      {
        value: "unannotated",
        label: `Undeclared ${counts.unannotated}`,
        tooltip: "Tools that declare nothing — authority unknown, not absent.",
      },
    ] as const
  ).filter((option) => option.value === "all" || counts[option.value] > 0);

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-2">
      {capabilities.length > TOOL_SEARCH_THRESHOLD && (
        <div className="flex flex-wrap items-center gap-2">
          <SegmentedControl
            value={filter}
            onChange={setFilter}
            options={options.map((option) => ({
              value: option.value as ToolFilter,
              label: option.label,
              ...("tooltip" in option ? { tooltip: option.tooltip } : {}),
            }))}
          />
          <SearchBar
            value={query}
            onChange={setQuery}
            placeholder="Find a tool"
            className="h-10 w-56"
          />
        </div>
      )}
      <div className="border-border max-h-96 min-h-0 flex-1 overflow-y-auto border">
        {ordered.length === 0 ? (
          <p className="text-muted-foreground px-3 py-6 text-center text-xs">
            No tool matches this filter. The server declares{" "}
            {capabilities.length}.
          </p>
        ) : (
          <ul className="divide-border divide-y">
            {ordered.map((tool) => (
              <li key={tool.tool} className={TOOL_ROW_CLASS}>
                <ToolRow tool={tool} />
              </li>
            ))}
          </ul>
        )}
      </div>
      {ordered.length !== capabilities.length && (
        <p className="text-muted-foreground text-xs">
          Showing {ordered.length} of {capabilities.length} declared tools.
        </p>
      )}
    </div>
  );
}

/** Below this many tools the list is short enough to read whole. */
const TOOL_SEARCH_THRESHOLD = 8;

function ProvenanceFacts({
  provenance,
}: {
  provenance: EvidenceProvenance;
}): JSX.Element {
  const facts: Array<{ label: string; value: React.ReactNode }> = [];
  if (provenance.registry) {
    facts.push({ label: "Catalogued in", value: provenance.registry });
  }
  if (provenance.specifier) {
    facts.push({ label: "Catalog entry", value: provenance.specifier });
  }
  facts.push({
    label: "Publisher vouched by the registry",
    value: provenance.official ? "yes" : "no",
  });
  if (provenance.status) {
    facts.push({ label: "Entry status", value: provenance.status });
  }
  if (provenance.publishedAt) {
    facts.push({
      label: "Version published",
      value: (
        <HumanizeDateTime
          date={new Date(provenance.publishedAt)}
          includeTime={false}
        />
      ),
    });
  }
  if (provenance.updatedAt) {
    facts.push({
      label: "Entry last updated",
      value: (
        <HumanizeDateTime
          date={new Date(provenance.updatedAt)}
          includeTime={false}
        />
      ),
    });
  }
  if (
    provenance.visitorsLastFourWeeks !== undefined &&
    provenance.visitorsLastFourWeeks > 0
  ) {
    facts.push({
      label: "Catalog visitors, last 4 weeks",
      value: provenance.visitorsLastFourWeeks.toLocaleString(),
    });
  }

  return <FactList facts={facts} />;
}

/** The provenance caveat, moved to its group's hint. */
const PROVENANCE_HINT =
  "The registry catalog's claims, not ours — a visitor count is a popularity proxy, not evidence about behavior.";

function MaturitySection({
  pkg,
  notPublished,
  packageName,
  provenance,
  identityKind,
  repository,
  repositoryNotFound,
}: {
  pkg: EvidencePackage | undefined;
  notPublished: boolean;
  packageName: string | undefined;
  provenance: EvidenceProvenance | undefined;
  identityKind: EvidenceIdentity["kind"];
  repository: EvidenceRepository | undefined;
  repositoryNotFound: boolean;
}): JSX.Element {
  if (identityKind === "remote" && provenance) {
    if (!provenance.catalogued) {
      return (
        <EvidenceGroup question="Is it real and maintained?" section="maturity">
          <AnswerBlock>
            No configured MCP registry catalogs this URL. The lookup ran cleanly
            — this is absence from the catalog, not a failed check.
          </AnswerBlock>
        </EvidenceGroup>
      );
    }
    return (
      <EvidenceGroup
        question="Is it real and maintained?"
        section="maturity"
        hint={PROVENANCE_HINT}
      >
        <ProvenanceFacts provenance={provenance} />
      </EvidenceGroup>
    );
  }

  if (notPublished) {
    return (
      <EvidenceGroup question="Is it real and maintained?" section="maturity">
        <AnswerBlock>
          The registry has no package named{" "}
          <code className="text-xs">{packageName ?? "this"}</code>. The lookup
          ran cleanly — this reference points at something its own registry does
          not know.
        </AnswerBlock>
      </EvidenceGroup>
    );
  }

  if (!pkg) {
    return (
      <EvidenceGroup question="Is it real and maintained?" section="maturity">
        <UnknownBlock>
          No registry metadata gathered — age, maintenance, and publishing
          history unknown.
        </UnknownBlock>
      </EvidenceGroup>
    );
  }

  const facts: Array<{ label: string; value: React.ReactNode }> = [];
  if (pkg.firstPublished) {
    facts.push({
      label: "First published",
      value: (
        <HumanizeDateTime
          date={new Date(pkg.firstPublished)}
          includeTime={false}
        />
      ),
    });
  }
  if (pkg.lastPublished) {
    facts.push({
      label: "Last release",
      value: (
        <HumanizeDateTime
          date={new Date(pkg.lastPublished)}
          includeTime={false}
        />
      ),
    });
  }
  if (pkg.versionCount !== undefined) {
    facts.push({ label: "Published versions", value: pkg.versionCount });
  }
  if (pkg.latestVersion) {
    facts.push({ label: "Latest version", value: pkg.latestVersion });
  }

  if (pkg.deprecated) {
    facts.unshift({
      label: "Current version",
      value: (
        <Absent>
          deprecated{pkg.deprecationReason ? ` — ${pkg.deprecationReason}` : ""}
        </Absent>
      ),
    });
  }
  if (repositoryNotFound) {
    facts.push({
      label: "Declared repository",
      value: <Absent>not found on the code host</Absent>,
    });
  }

  return (
    <EvidenceGroup question="Is it real and maintained?" section="maturity">
      <FactList facts={facts} />
      {repository && <RepositoryFacts repository={repository} />}
    </EvidenceGroup>
  );
}

/**
 * The declared repository's public track record. Everything here is about the
 * repository the publisher chose to name — nothing verifies that repository
 * builds this package, so a popular repository must not read as vouching for
 * the artifact.
 */
function RepositoryFacts({
  repository,
}: {
  repository: EvidenceRepository;
}): JSX.Element {
  const facts: Array<{ label: string; value: React.ReactNode }> = [
    {
      label: "Declared repository",
      value: `${repository.owner}/${repository.name}`,
    },
  ];
  if (repository.stars !== undefined) {
    facts.push({ label: "Stars", value: repository.stars });
  }
  if (repository.forks !== undefined) {
    facts.push({ label: "Forks", value: repository.forks });
  }
  if (
    repository.contributorCount !== undefined &&
    repository.contributorCount > 0
  ) {
    facts.push({ label: "Contributors", value: repository.contributorCount });
  }
  if (repository.openIssues !== undefined) {
    facts.push({ label: "Open issues and PRs", value: repository.openIssues });
  }
  if (repository.createdAt) {
    facts.push({
      label: "Repository created",
      value: (
        <HumanizeDateTime
          date={new Date(repository.createdAt)}
          includeTime={false}
        />
      ),
    });
  }
  if (repository.pushedAt) {
    facts.push({
      label: "Last commit pushed",
      value: (
        <HumanizeDateTime
          date={new Date(repository.pushedAt)}
          includeTime={false}
        />
      ),
    });
  }

  if (repository.archived) {
    facts.push({ label: "Repository state", value: <Absent>archived</Absent> });
  }

  return (
    <>
      <FactList facts={facts} />
      <p className="text-muted-foreground text-xs">
        The repository is the publisher's claim; nothing verifies it builds the
        package that installs.
      </p>
    </>
  );
}

/**
 * OSV's answer gets its own group: checked-and-clean, advisories-found, and
 * could-not-check are three different answers, and collapsing any two of them
 * is exactly what this panel exists to prevent.
 *
 * Only rendered for a package. Advisory databases index published packages, so
 * for a hosted endpoint this group could only ever say the database has
 * nothing to look up — which the signals panel records as an unknown instead of
 * spending half a row on it for every remote server ever reviewed.
 */
function AdvisoriesSection({
  advisories,
}: {
  advisories: EvidenceAdvisories | undefined;
}): JSX.Element {
  if (!advisories) {
    return (
      <EvidenceGroup
        question="Does anything published say it's vulnerable?"
        section="advisories"
      >
        <UnknownBlock>
          No advisory database was consulted — published vulnerabilities are
          unknown.
        </UnknownBlock>
      </EvidenceGroup>
    );
  }

  if (advisories.knownCount === 0) {
    return (
      <EvidenceGroup
        question="Does anything published say it's vulnerable?"
        section="advisories"
      >
        <AnswerBlock>
          OSV lists no published advisories for this package. Checked today and
          clean — not a guarantee, and it says nothing about unreported issues.
        </AnswerBlock>
      </EvidenceGroup>
    );
  }

  const sampled = advisories.advisories.length;
  return (
    <EvidenceGroup
      question="Does anything published say it's vulnerable?"
      section="advisories"
      note={
        sampled < advisories.knownCount && (
          <span className="text-muted-foreground text-xs">
            most recent {sampled} of {advisories.knownCount} shown
          </span>
        )
      }
    >
      <AdvisoryList advisories={advisories.advisories} />
    </EvidenceGroup>
  );
}

function AdvisoryList({
  advisories,
}: {
  advisories: EvidenceAdvisoryItem[];
}): JSX.Element {
  return (
    <CollapsibleList
      items={advisories}
      itemKey={(advisory) => advisory.id}
      itemClassName="px-3 py-1.5 text-xs"
      noun="advisories"
      renderItem={(advisory) => (
        <>
          <div className="flex items-center justify-between gap-3">
            <span className="font-mono">{advisory.id}</span>
            {advisory.severity && (
              <Badge variant="destructive">{advisory.severity}</Badge>
            )}
          </div>
          {advisory.summary && (
            <p className="text-muted-foreground mt-0.5">{advisory.summary}</p>
          )}
        </>
      )}
    />
  );
}

export function StatusBadge({ status }: { status: string }): JSX.Element {
  switch (status) {
    case "approved":
      return <Badge variant="success">Approved</Badge>;
    case "denied":
      return <Badge variant="destructive">Denied</Badge>;
    case "requested":
      return <Badge variant="information">Awaiting decision</Badge>;
    case "superseded":
      // Neutral, not destructive: the history stands, nothing is pending.
      // The default (softest) neutral border washes out against the table
      // backgrounds, hence the stronger theme-aware border.
      return (
        <Badge variant="neutral" className="border-neutral-default">
          Superseded
        </Badge>
      );
    case "unreviewed":
      // "Unreviewed", not "Review requested": this state means a dossier was
      // gathered and nobody has asked for anything — the request states are
      // "requested" (awaiting a decision) and the two decided ones. Labelling
      // it as a request contradicted the very notice under it saying no one
      // had asked.
      // The token palette has no yellow family; the stock yellow scale keeps
      // this state visually distinct from destructive-adjacent orange.
      return (
        <Badge
          variant="warning"
          className="border-yellow-300 text-yellow-600 dark:border-yellow-800 dark:text-yellow-500"
        >
          Unreviewed
        </Badge>
      );
    default:
      return <Badge variant="neutral">{status}</Badge>;
  }
}
