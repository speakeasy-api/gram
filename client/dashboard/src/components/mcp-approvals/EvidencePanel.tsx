import { Badge } from "@/components/ui/Badge";
import { Heading } from "@/components/ui/Heading";
import { HumanizeDateTime } from "@/lib/dates";
import { cn } from "@/lib/utils";
import { ChevronDown, ChevronUp } from "lucide-react";
import { useState } from "react";
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
  collectedAt,
}: {
  document: EvidenceDocument | null;
  collectedAt: Date | undefined;
}): JSX.Element {
  if (!document) {
    return (
      <UnknownBlock>
        No evidence gathered. Nothing is known — which is not the same as
        nothing being wrong.
      </UnknownBlock>
    );
  }

  return (
    <div className="space-y-4">
      {document.gaps.length > 0 && <GapsNotice gaps={document.gaps} />}
      {collectedAt && (
        <p className="text-muted-foreground text-xs">
          Gathered <HumanizeDateTime date={collectedAt} />. Declared by the
          server or its registry, or seen in this organization's traffic —
          nothing is verified behavior.
        </p>
      )}
      <TrustSection
        identity={document.identity}
        pkg={document.package}
        domain={document.domain}
      />
      <AuthoritySection authority={document.authority} />
      <DeclaredCapabilitySection
        capabilities={document.capabilities}
        source={document.capabilitiesSource}
      />
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
      <ExposureSection
        exposure={document.exposure}
        identity={document.identity}
      />
      <OrgKnowledgeSection />
    </div>
  );
}

function EvidenceGroup({
  question,
  children,
}: {
  question: string;
  children: React.ReactNode;
}): JSX.Element {
  return (
    <section className="space-y-1.5">
      {/* The questions are the page's real structure, so they keep the serif
          treatment content subsections use — sized down so a full gather fits
          on one screen without zooming. */}
      <Heading variant="h3" className="text-lg font-thin">
        {question}
      </Heading>
      {children}
    </section>
  );
}

/**
 * The visual for "we know nothing about this": a dashed hairline frame that
 * looks conspicuously empty. Deliberately distinct from both an error state
 * and a populated card, and never green.
 */
function UnknownBlock({
  children,
}: {
  children: React.ReactNode;
}): JSX.Element {
  return (
    <div className="border-border text-muted-foreground border border-dashed px-2.5 py-1.5 text-xs">
      {children}
    </div>
  );
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
        "grid grid-cols-1 gap-x-6 gap-y-1 px-3 py-2 sm:grid-cols-2",
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

function authorityModeLabel(mode: string): string {
  switch (mode) {
    case "oauth":
      return "OAuth — delegated, scoped, revocable";
    case "api_key":
      return "Static secret pasted at install";
    case "none":
      return "No credential requirement published";
    default:
      return "Undeclared";
  }
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
          Not gathered. Unknown — not "requires nothing".
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
            <div className="flex flex-wrap gap-1">
              {authority.scopes.map((scope) => (
                <span
                  key={scope}
                  className="border-border border px-1.5 py-px font-mono text-xs"
                >
                  {scope}
                </span>
              ))}
            </div>
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
        <EvidenceGroup question="What does it say it can do?">
          <div className="border-border border px-2.5 py-1.5 text-xs">
            {source === "registry"
              ? "The registry catalog's copy declares no tools."
              : "The server answered the listing with zero tools."}{" "}
            The listing succeeded — this is a declared-empty toolset, not a
            failed check.
          </div>
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
    <EvidenceGroup question="What does it say it can do?">
      <p className="text-muted-foreground text-xs">
        {capabilitySourceNote(source)}
      </p>
      {actingTools.length > 0 && (
        <div className="border-warning border px-2.5 py-1.5 text-xs">
          {actingTools.length === 1
            ? "One tool declares"
            : `${actingTools.length} of ${capabilities.length} tools declare`}{" "}
          acting on your behalf.
        </div>
      )}
      <ToolList capabilities={capabilities} />
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
  const [expanded, setExpanded] = useState(false);
  const collapsible = items.length > previewCount;
  const visible =
    collapsible && !expanded ? items.slice(0, previewCount) : items;

  return (
    <div className="border-border border">
      <ul className="divide-border divide-y">
        {visible.map((item) => (
          <li key={itemKey(item)} className={itemClassName}>
            {renderItem(item)}
          </li>
        ))}
      </ul>
      {collapsible && (
        <button
          type="button"
          aria-expanded={expanded}
          onClick={() => setExpanded((current) => !current)}
          className="text-muted-foreground hover:text-foreground border-border flex w-full items-center justify-center gap-1 border-t px-3 py-1 text-xs"
        >
          {expanded ? (
            <>
              Show fewer
              <ChevronUp className="size-3" />
            </>
          ) : (
            <>
              Show all {items.length} {noun}
              <ChevronDown className="size-3" />
            </>
          )}
        </button>
      )}
    </div>
  );
}

function ToolList({
  capabilities,
}: {
  capabilities: EvidenceCapability[];
}): JSX.Element {
  return (
    <CollapsibleList
      items={capabilities}
      itemKey={(tool) => tool.tool}
      itemClassName="flex flex-wrap items-center justify-between gap-2 px-3 py-1 text-xs"
      noun="tools"
      renderItem={(tool) => (
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
      )}
    />
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

  return (
    <>
      <p className="text-muted-foreground text-xs">
        The registry catalog's claims, not ours — a visitor count is a
        popularity proxy, not evidence about behavior.
      </p>
      <FactList facts={facts} />
    </>
  );
}

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
          <div className="border-border border px-2.5 py-1.5 text-xs">
            No configured MCP registry catalogs this URL. The lookup ran cleanly
            — this is absence from the catalog, not a failed check.
          </div>
        </EvidenceGroup>
      );
    }
    return (
      <EvidenceGroup question="Is it real and maintained?">
        <ProvenanceFacts provenance={provenance} />
      </EvidenceGroup>
    );
  }

  if (notPublished) {
    return (
      <EvidenceGroup question="Is it real and maintained?">
        <div className="border-border border px-2.5 py-1.5 text-xs">
          The registry has no package named{" "}
          <code className="text-xs">{packageName ?? "this"}</code>. The lookup
          ran cleanly — this reference points at something its own registry does
          not know.
        </div>
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
        <div className="border-border border px-2.5 py-1.5 text-xs">
          OSV lists no published advisories for this package. Checked today and
          clean — not a guarantee, and it says nothing about unreported issues.
        </div>
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

function ExposureSection({
  exposure,
  identity,
}: {
  exposure: EvidenceExposure | undefined;
  identity: EvidenceIdentity;
}): JSX.Element {
  if (!exposure) {
    const reason =
      identity.kind === "remote"
        ? "Usage records could not be gathered."
        : "No URL to look up in usage records — exposure here is unknowable from traffic.";
    return (
      <EvidenceGroup question="Are we already exposed?">
        <UnknownBlock>{reason}</UnknownBlock>
      </EvidenceGroup>
    );
  }

  if (exposure.status === "unseen") {
    return (
      <EvidenceGroup question="Are we already exposed?">
        <div className="border-border border px-2.5 py-1.5 text-xs">
          No one in this project has recorded traffic to this server. Denying it
          costs nobody an existing workflow.
        </div>
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
    facts.push({ label: "Known here as", value: exposure.serverName });
  }

  return (
    <EvidenceGroup question="Are we already exposed?">
      {exposure.inUse && (
        <div className="border-warning border px-2.5 py-1.5 text-xs">
          <span className="font-medium">
            This server is already in use in this project.
          </span>{" "}
          <span className="text-muted-foreground">
            A denial changes existing workflows, not just future ones.
          </span>
        </div>
      )}
      <FactList facts={facts} />
    </EvidenceGroup>
  );
}

function OrgKnowledgeSection(): JSX.Element {
  return (
    <EvidenceGroup question="What do we already know?">
      <UnknownBlock>
        Nothing on file — existing contract and prior security review are
        unrecorded.
      </UnknownBlock>
    </EvidenceGroup>
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
    case "unreviewed":
      // The token palette has no yellow family; the stock yellow scale keeps
      // this state visually distinct from destructive-adjacent orange.
      return (
        <Badge
          variant="warning"
          className="border-yellow-300 text-yellow-600 dark:border-yellow-800 dark:text-yellow-500"
        >
          Review requested
        </Badge>
      );
    default:
      return <Badge variant="neutral">{status}</Badge>;
  }
}
