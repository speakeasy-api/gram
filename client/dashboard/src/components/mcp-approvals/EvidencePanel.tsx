import { Badge } from "@/components/ui/Badge";
import { Heading } from "@/components/ui/Heading";
import { HumanizeDateTime } from "@/lib/dates";
import { cn } from "@/lib/utils";
import type {
  EvidenceAuthority,
  EvidenceCapability,
  EvidenceDocument,
  EvidenceExposure,
  EvidenceIdentity,
  EvidencePackage,
  EvidenceProvenance,
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
      <TrustSection identity={document.identity} pkg={document.package} />
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
          <dd className="border-border max-w-full min-w-0 border px-1.5 py-px text-right font-mono text-xs break-all">
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
}: {
  identity: EvidenceIdentity;
  pkg: EvidencePackage | undefined;
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

  return (
    <EvidenceGroup question="Who am I trusting?">
      <FactList facts={facts} />
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
          Answers without any credential:{" "}
          <span className="font-mono">
            {authority.unauthenticatedTools.join(", ")}
          </span>
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
            : `${actingTools.length} tools declare`}{" "}
          acting on your behalf:{" "}
          <span className="font-mono">
            {actingTools.map((tool) => tool.tool).join(", ")}
          </span>
        </div>
      )}
      <ul className="border-border divide-border divide-y border">
        {capabilities.map((tool) => (
          <li
            key={tool.tool}
            className="flex flex-wrap items-center justify-between gap-2 px-3 py-1 text-xs"
          >
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
          </li>
        ))}
      </ul>
    </EvidenceGroup>
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
}: {
  pkg: EvidencePackage | undefined;
  notPublished: boolean;
  packageName: string | undefined;
  provenance: EvidenceProvenance | undefined;
  identityKind: EvidenceIdentity["kind"];
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
    </EvidenceGroup>
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
    default:
      return <Badge variant="neutral">{status}</Badge>;
  }
}
