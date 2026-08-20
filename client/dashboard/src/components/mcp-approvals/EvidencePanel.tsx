import {
  authorityModeLabel,
  USAGE_QUESTION,
} from "@/components/mcp-approvals/evidence";
import { Badge } from "@/components/ui/Badge";
import { Heading } from "@/components/ui/Heading";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import { Info } from "lucide-react";
import { useId } from "react";
import { HumanizeDateTime } from "@/lib/dates";
import {
  MoreToggle,
  useCollapsedPreview,
} from "@/components/ui/collapsible-preview";
import { cn } from "@/lib/utils";
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
import { gapLabel } from "./evidence";

/**
 * The evidence panel, grouped by the question the admin is asking rather than
 * by where the data came from.
 *
 * The one rule that shapes everything here: unknown must read as unknown.
 * A group with no gathered data renders a conspicuously empty block, never a
 * clean or reassuring state — an absence of evidence is not evidence of
 * safety, and a failed lookup is listed as a gap rather than silently omitted.
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

  // A gap whose own group already renders an unknown block is being reported
  // twice: the banner said "the tool listing could not be read" directly above
  // a box saying "No tool declarations gathered". The banner is for sources
  // whose failure has nowhere else to surface — exposure, the code host, the
  // domain registry — which leave no visible hole of their own.
  const unshownGaps = document.gaps.filter(
    (gap) => !GAPS_SHOWN_BY_THEIR_OWN_GROUP.has(gap),
  );

  return (
    <div className="space-y-3">
      {unshownGaps.length > 0 && <GapsNotice gaps={unshownGaps} />}
      {/* Two columns on a wide screen. Each question is short enough that
          stacking them all made the page scroll for no reason; the reading
          order still runs left to right, identity first. */}
      <div className="grid gap-x-6 gap-y-3 lg:grid-cols-2">
        <TrustSection
          identity={document.identity}
          pkg={document.package}
          domain={document.domain}
        />
        <AuthoritySection authority={document.authority} />
        {usage ? (
          <>
            <DeclaredCapabilitySection
              capabilities={document.capabilities}
              source={document.capabilitiesSource}
              fill
            />
            {/* The traffic table answers "are we already exposed?" on its own —
                names, counts and recency — so that question no longer has a
                group of its own. What the table cannot say is what a denial
                would cost, so that judgment rides here as the note. */}
            <EvidenceGroup
              question={USAGE_QUESTION}
              note={
                document.exposure?.inUse && (
                  <span className="border-warning border px-2.5 py-1 text-xs">
                    Already in use — a denial changes existing workflows.
                  </span>
                )
              }
            >
              {usage}
            </EvidenceGroup>
          </>
        ) : (
          <>
            <DeclaredCapabilitySection
              capabilities={document.capabilities}
              source={document.capabilitiesSource}
            />
            {/* Without a traffic table the exposure figures have nowhere else
                to appear: the review sheet has no summary strip, and a frozen
                decision snapshot is evidence as it stood, not traffic as it is
                now. The detail page drops this group because its strip and
                table already answer it. */}
            <ExposureSection
              exposure={document.exposure}
              identity={document.identity}
            />
          </>
        )}
        {/* Maturity and advisories share the last row. Maturity used to span
            the full width so its fact list could run two columns, but with
            the exposure group gone an odd number of groups left a hole beside
            whichever one ran last — and three even rows read better than two
            rows and two banners. Its list falls back to one column here via
            its own container query. */}
        <MaturitySection
          pkg={document.package}
          notPublished={document.packageNotPublished}
          packageName={document.identity.packageName}
          provenance={document.provenance}
          identityKind={document.identity.kind}
          repository={document.repository}
          repositoryNotFound={document.repositoryNotFound}
        />
        <AdvisoriesSection
          advisories={document.advisories}
          identityKind={document.identity.kind}
        />
      </div>
    </div>
  );
}

/**
 * Failed lookups whose group answers for them, so the banner does not repeat
 * what the reader is already looking at. Each one maps to a group that renders
 * its own unknown block when the data is missing: authority and capabilities
 * to their questions, package and catalog to "is it real and maintained?",
 * advisories to the vulnerability question.
 *
 * Deliberately not here: exposure, repository and domain failures. Those leave
 * facts quietly absent from a list rather than an empty box, so the banner is
 * the only place they are ever stated.
 */
const GAPS_SHOWN_BY_THEIR_OWN_GROUP = new Set([
  "authority_probe_failed",
  "tool_declarations_probe_failed",
  "package_lookup_failed",
  "catalog_lookup_failed",
  "advisory_lookup_failed",
]);

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
      <EvidenceGroup question="Are we already exposed?">
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
      <EvidenceGroup question="Are we already exposed?">
        <AnswerBlock>
          No one in this project has recorded traffic to this server. Denying it
          costs nobody an existing workflow.
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
    <EvidenceGroup
      question="Are we already exposed?"
      note={
        exposure.inUse && (
          <span className="border-warning border px-2.5 py-1 text-xs">
            Already in use — a denial changes existing workflows.
          </span>
        )
      }
    >
      <FactList facts={facts} />
    </EvidenceGroup>
  );
}

export function EvidenceGroup({
  question,
  note,
  hint,
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
    <section className="@container flex h-full flex-col gap-1.5">
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

function GapsNotice({ gaps }: { gaps: string[] }): JSX.Element {
  return (
    <div className="border-warning border px-2.5 py-1.5 text-xs">
      {gaps.map((gap) => (
        <p key={gap}>{gapLabel(gap)} — treat as unknown, not clean.</p>
      ))}
    </div>
  );
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
      <EvidenceGroup question="Who am I trusting?">
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
  }
  if (domain?.registrar) {
    facts.push({ label: "Registrar", value: domain.registrar });
  }

  return (
    <EvidenceGroup question="Who am I trusting?">
      <FactList facts={facts} />
      {domain?.unregistered && (
        <div className="border-warning border px-2.5 py-1.5 text-xs">
          The domain registry reports no registration for{" "}
          <span className="font-mono">{domain.domain}</span> — unusual for a
          host that answers traffic.
        </div>
      )}
      {identity.kind === "remote" && !identity.registrableDomain && (
        <UnknownBlock>
          No registrable public domain — nothing links this host to a known
          publisher.
        </UnknownBlock>
      )}
    </EvidenceGroup>
  );
}

function AuthoritySection({
  authority,
}: {
  authority: EvidenceAuthority | undefined;
}): JSX.Element {
  if (!authority || authority.undeclared) {
    return (
      <EvidenceGroup question="What is it asking me to hand over?">
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
    <EvidenceGroup question="What is it asking me to hand over?">
      {authority.demandedSecrets.map((secret) => (
        <div
          key={secret.name}
          className="border-warning border px-2.5 py-1.5 text-xs"
        >
          Requires you to hand over a secret named{" "}
          <span className="font-mono">{secret.name}</span>
          {secret.description && (
            <span className="text-muted-foreground">
              {" "}
              — "{secret.description}"
            </span>
          )}
        </div>
      ))}
      {authority.optionalSecrets.length > 0 && (
        <div className="border-border text-muted-foreground border px-2.5 py-1.5 text-xs">
          Accepts optional secrets:{" "}
          <span className="text-foreground font-mono">
            {authority.optionalSecrets.map((secret) => secret.name).join(", ")}
          </span>
        </div>
      )}
      <div className="border-border border">
        <FactList facts={facts} bare />
        {authority.scopes.length > 0 && (
          <div className="border-border border-t px-3 py-2">
            <p className="text-muted-foreground mb-1.5 text-xs">
              Scopes it will ask to be granted — the one item here the
              authorization server actually enforces:
            </p>
            <ScopeChips scopes={authority.scopes} />
          </div>
        )}
      </div>
      {authority.unauthenticatedTools.length > 0 && (
        <div className="border-warning border px-2.5 py-1.5 text-xs">
          Listed {authority.unauthenticatedTools.length}{" "}
          {authority.unauthenticatedTools.length === 1 ? "tool" : "tools"} to an
          unauthenticated caller — the protocol answered without any credential.
        </div>
      )}
    </EvidenceGroup>
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
  fill = false,
}: {
  capabilities: EvidenceCapability[];
  source: "server" | "registry" | undefined;
  /** Share a row with another group, and size the tool list to it. */
  fill?: boolean;
}): JSX.Element {
  if (capabilities.length === 0) {
    // A source that answered with zero tools is a real declaration —
    // rendered as such, never as a failed gather.
    if (source) {
      return (
        <EvidenceGroup question="What does it say it can do?">
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
      <EvidenceGroup question="What does it say it can do?">
        <UnknownBlock>
          No tool declarations gathered. Silence is not harmlessness.
        </UnknownBlock>
      </EvidenceGroup>
    );
  }

  const actingTools = capabilities.filter((tool) => tool.actsOnBehalf);

  return (
    <EvidenceGroup
      question="What does it say it can do?"
      hint={`${capabilitySourceNote(source)} ${
        capabilities.length === 1
          ? "1 tool declared."
          : `${capabilities.length} tools declared.`
      }`}
      note={
        actingTools.length > 0 && (
          <span className="border-warning border px-2.5 py-1 text-xs">
            {actingTools.length === 1
              ? "One tool declares"
              : `${actingTools.length} of ${capabilities.length} tools declare`}{" "}
            acting on your behalf.
          </span>
        )
      }
    >
      <ToolList capabilities={capabilities} fill={fill} />
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

/**
 * Every declared tool. Sharing a row with another group, the list takes
 * whatever height that group settled on and scrolls past it — a fixed preview
 * count would be a guess about the neighbour, right for one server's traffic
 * and wrong for the next. The scroller is absolutely positioned because a
 * grid row is sized by its content: left in flow, the list would set the
 * height it is supposed to be reading.
 */
function ToolList({
  capabilities,
  fill,
}: {
  capabilities: EvidenceCapability[];
  fill: boolean;
}): JSX.Element {
  if (!fill) {
    return (
      <CollapsibleList
        items={capabilities}
        itemKey={(tool) => tool.tool}
        itemClassName={TOOL_ROW_CLASS}
        noun="tools"
        renderItem={(tool) => <ToolRow tool={tool} />}
      />
    );
  }

  return (
    <div className="border-border relative min-h-40 flex-1 border">
      <ul className="divide-border absolute inset-0 divide-y overflow-y-auto">
        {capabilities.map((tool) => (
          <li key={tool.tool} className={TOOL_ROW_CLASS}>
            <ToolRow tool={tool} />
          </li>
        ))}
      </ul>
    </div>
  );
}

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
        <EvidenceGroup question="Is it real and maintained?">
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
        hint={PROVENANCE_HINT}
      >
        <ProvenanceFacts provenance={provenance} />
      </EvidenceGroup>
    );
  }

  if (notPublished) {
    return (
      <EvidenceGroup question="Is it real and maintained?">
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
      <EvidenceGroup question="Is it real and maintained?">
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

  return (
    <EvidenceGroup question="Is it real and maintained?">
      {pkg.deprecated && (
        <div className="border-destructive text-foreground border px-2.5 py-1.5 text-xs">
          <span className="font-medium">
            The registry marks the current version deprecated
          </span>
          {pkg.deprecationReason && (
            <span className="text-muted-foreground">
              {" "}
              — "{pkg.deprecationReason}"
            </span>
          )}
        </div>
      )}
      <FactList facts={facts} />
      {repository && <RepositoryFacts repository={repository} />}
      {repositoryNotFound && (
        <div className="border-warning border px-2.5 py-1.5 text-xs">
          The publisher declares a source repository that does not exist on the
          code host — the package's provenance cannot be traced to any source.
        </div>
      )}
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

  return (
    <>
      {repository.archived && (
        <div className="border-warning border px-2.5 py-1.5 text-xs">
          The declared repository is archived — its owner froze it against
          further commits and issues.
        </div>
      )}
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
 * is exactly what this panel exists to prevent. Advisory databases index
 * published packages, so a non-package reference renders the group with an
 * explanation rather than an answer — the question still exists for a remote
 * endpoint; a database just cannot answer it.
 */
function AdvisoriesSection({
  advisories,
  identityKind,
}: {
  advisories: EvidenceAdvisories | undefined;
  identityKind: EvidenceIdentity["kind"];
}): JSX.Element {
  if (identityKind !== "package") {
    return (
      <EvidenceGroup question="Does anything published say it's vulnerable?">
        <UnknownBlock>
          Advisory databases index published packages, and this server is not
          one — there is nothing to look up. The vendor's security history is a
          research question, not a database check.
        </UnknownBlock>
      </EvidenceGroup>
    );
  }

  if (!advisories) {
    return (
      <EvidenceGroup question="Does anything published say it's vulnerable?">
        <UnknownBlock>
          No advisory database was consulted — published vulnerabilities are
          unknown.
        </UnknownBlock>
      </EvidenceGroup>
    );
  }

  if (advisories.knownCount === 0) {
    return (
      <EvidenceGroup question="Does anything published say it's vulnerable?">
        <AnswerBlock>
          OSV lists no published advisories for this package. Checked today and
          clean — not a guarantee, and it says nothing about unreported issues.
        </AnswerBlock>
      </EvidenceGroup>
    );
  }

  const sampled = advisories.advisories.length;
  return (
    <EvidenceGroup question="Does anything published say it's vulnerable?">
      <div className="border-destructive border px-2.5 py-1.5 text-xs">
        <span className="font-medium">
          {advisories.knownCount === 1
            ? "1 published advisory names this package"
            : `${advisories.knownCount} published advisories name this package`}
        </span>
        {sampled < advisories.knownCount && (
          <span className="text-muted-foreground">
            {" "}
            — most recent {sampled} shown
          </span>
        )}
      </div>
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
      // A decided review whose decision an admin explicitly displaced from
      // the policy editor. Neutral, not destructive: the history stands and
      // nothing is pending — enforcement just no longer derives from it.
      return <Badge variant="neutral">Superseded</Badge>;
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
