/**
 * Client-side reading of the version-1 evidence document assembled at intake
 * (server/internal/mcpapproval/evidence). Everything in it is a declaration by
 * the server or its registry, or an observation of this org's own traffic —
 * never a claim about the server's behavior, and the panel's copy must keep
 * that distinction.
 *
 * Parsing is deliberately tolerant: an unrecognized version or shape yields
 * null, and the panel renders "not gathered" — unknown must read as unknown,
 * never as an error and never as clean.
 */

const SUPPORTED_EVIDENCE_VERSION = 1;

export type EvidenceIdentity = {
  kind: "remote" | "package" | "unresolved";
  artifactRef?: string;
  versionPinned: boolean;
  host?: string;
  registrableDomain?: string;
  registry?: string;
  packageName?: string;
  packageVersion?: string;
};

export type EvidencePackage = {
  registry: string;
  name: string;
  license?: string;
  latestVersion?: string;
  firstPublished?: string;
  lastPublished?: string;
  versionCount?: number;
  maintainerCount?: number;
  deprecated?: boolean;
  deprecationReason?: string;
};

export type EvidenceRepository = {
  url: string;
  host: string;
  owner: string;
  name: string;
  stars?: number;
  forks?: number;
  /** GitHub counts open issues and pull requests together. */
  openIssues?: number;
  archived: boolean;
  createdAt?: string;
  pushedAt?: string;
  /** Absent when the host did not answer — unknown, never "no contributors". */
  contributorCount?: number;
};

export type EvidenceAdvisoryItem = {
  id: string;
  summary?: string;
  severity?: string;
  published?: string;
};

export type EvidenceAdvisories = {
  ecosystem?: string;
  package?: string;
  /** Everything the database returned; `advisories` is a recent sample. */
  knownCount: number;
  advisories: EvidenceAdvisoryItem[];
};

export type EvidenceDomain = {
  domain: string;
  registeredAt?: string;
  registrar?: string;
  /** The registry answered that no such domain exists. */
  unregistered: boolean;
};

export type EvidenceExposure = {
  status: "seen" | "unseen";
  canonicalUrl?: string;
  urlHost?: string;
  serverName?: string;
  firstSeen?: string;
  lastSeen?: string;
  firstCalled?: string;
  lastCalled?: string;
  callCount?: number;
  userCount?: number;
  inUse: boolean;
};

type EvidenceCredential = {
  name: string;
  required: boolean;
  description?: string;
};

export type EvidenceAuthority = {
  mode: string;
  transport?: string;
  scopes: string[];
  dynamicRegistration: boolean;
  demandedSecrets: EvidenceCredential[];
  optionalSecrets: EvidenceCredential[];
  unauthenticatedTools: string[];
  undeclared: boolean;
};

export type EvidenceCapability = {
  tool: string;
  declared: string[];
  schemaImplied: string[];
  actsOnBehalf: boolean;
  unannotated: boolean;
};

export type EvidenceProvenance = {
  registry?: string;
  specifier?: string;
  catalogued: boolean;
  official: boolean;
  status?: string;
  isLatest: boolean;
  publishedAt?: string;
  updatedAt?: string;
  visitorsLastWeek?: number;
  visitorsLastFourWeeks?: number;
  visitorsTotal?: number;
};

export type EvidenceDocument = {
  identity: EvidenceIdentity;
  package?: EvidencePackage;
  packageNotPublished: boolean;
  /**
   * The code host's numbers for the repository the publisher declared. The
   * repository is the publisher's claim — nothing verifies it builds this
   * package — and the panel's copy must say so.
   */
  repository?: EvidenceRepository;
  /** The publisher declared a repository and the code host has no such repository. */
  repositoryNotFound: boolean;
  advisories?: EvidenceAdvisories;
  domain?: EvidenceDomain;
  exposure?: EvidenceExposure;
  authority?: EvidenceAuthority;
  capabilities: EvidenceCapability[];
  /**
   * Where the capability declarations came from: the server's own
   * unauthenticated tools/list, or the registry catalog's copy of them —
   * one step further from the source, and the panel must say so.
   */
  capabilitiesSource?: "server" | "registry";
  provenance?: EvidenceProvenance;
  gaps: string[];
};

/** Human copy for the gap identifiers the assembler records. */
export function gapLabel(gap: string): string {
  switch (gap) {
    case "package_lookup_failed":
      return "The package registry could not be reached, so its metadata is unknown";
    case "exposure_lookup_failed":
      return "Usage records could not be read, so exposure in this organization is unknown";
    case "authority_probe_failed":
      return "The server's authentication metadata could not be read, so what it asks for is unknown";
    case "tool_declarations_probe_failed":
      return "Neither the server nor a registry catalog supplied a tool listing, so its declared capabilities are unknown";
    case "catalog_lookup_failed":
      return "The MCP registry catalog could not be consulted, so whether this server is catalogued is unknown";
    case "repository_lookup_failed":
      return "The code host could not be reached, so the declared repository's track record is unknown";
    case "advisory_lookup_failed":
      return "The vulnerability database could not be queried, so published advisories are unknown";
    case "domain_lookup_failed":
      return "The domain registry could not be consulted, so the domain's registration age is unknown";
    default:
      return `A source could not be consulted (${gap})`;
  }
}

function asRecord(value: unknown): Record<string, unknown> | null {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    return null;
  }
  return value as Record<string, unknown>;
}

function str(record: Record<string, unknown>, key: string): string | undefined {
  const value = record[key];
  return typeof value === "string" && value !== "" ? value : undefined;
}

function num(record: Record<string, unknown>, key: string): number | undefined {
  const value = record[key];
  return typeof value === "number" ? value : undefined;
}

function bool(record: Record<string, unknown>, key: string): boolean {
  return record[key] === true;
}

function identityKind(value: string | undefined): EvidenceIdentity["kind"] {
  if (value === "remote" || value === "package") return value;
  return "unresolved";
}

/**
 * Reads the evidence blob from `getRequest`. Returns null when the document
 * is absent, from an unsupported version, or not shaped like a document —
 * every case the panel must render as "not gathered".
 */
export function parseEvidenceDocument(
  evidence: unknown,
  evidenceVersion: number | undefined,
): EvidenceDocument | null {
  if (evidenceVersion !== SUPPORTED_EVIDENCE_VERSION) return null;

  const root = asRecord(evidence);
  if (!root) return null;

  const identityRecord = asRecord(root["identity"]);
  if (!identityRecord) return null;

  const identity: EvidenceIdentity = {
    kind: identityKind(str(identityRecord, "kind")),
    artifactRef: str(identityRecord, "artifact_ref"),
    versionPinned: bool(identityRecord, "version_pinned"),
    host: str(identityRecord, "host"),
    registrableDomain: str(identityRecord, "registrable_domain"),
    registry: str(identityRecord, "registry"),
    packageName: str(identityRecord, "package_name"),
    packageVersion: str(identityRecord, "package_version"),
  };

  let packageSection: EvidencePackage | undefined;
  const packageRecord = asRecord(root["package"]);
  if (packageRecord) {
    packageSection = {
      registry: str(packageRecord, "registry") ?? "",
      name: str(packageRecord, "name") ?? "",
      license: str(packageRecord, "license"),
      latestVersion: str(packageRecord, "latest_version"),
      firstPublished: str(packageRecord, "first_published"),
      lastPublished: str(packageRecord, "last_published"),
      versionCount: num(packageRecord, "version_count"),
      maintainerCount: num(packageRecord, "maintainer_count"),
      deprecated: bool(packageRecord, "deprecated"),
      deprecationReason: str(packageRecord, "deprecation_reason"),
    };
  }

  let repository: EvidenceRepository | undefined;
  const repositoryRecord = asRecord(root["repository"]);
  if (repositoryRecord) {
    repository = {
      url: str(repositoryRecord, "url") ?? "",
      host: str(repositoryRecord, "host") ?? "",
      owner: str(repositoryRecord, "owner") ?? "",
      name: str(repositoryRecord, "name") ?? "",
      stars: num(repositoryRecord, "stars"),
      forks: num(repositoryRecord, "forks"),
      openIssues: num(repositoryRecord, "open_issues"),
      archived: bool(repositoryRecord, "archived"),
      createdAt: str(repositoryRecord, "created_at"),
      pushedAt: str(repositoryRecord, "pushed_at"),
      contributorCount: num(repositoryRecord, "contributor_count"),
    };
  }

  let advisoriesSection: EvidenceAdvisories | undefined;
  const advisoriesRecord = asRecord(root["advisories"]);
  if (advisoriesRecord) {
    advisoriesSection = {
      ecosystem: str(advisoriesRecord, "ecosystem"),
      package: str(advisoriesRecord, "package"),
      knownCount: num(advisoriesRecord, "known_count") ?? 0,
      advisories: advisoryItems(advisoriesRecord["advisories"]),
    };
  }

  let domain: EvidenceDomain | undefined;
  const domainRecord = asRecord(root["domain"]);
  const domainName = domainRecord ? str(domainRecord, "domain") : undefined;
  if (domainRecord && domainName) {
    domain = {
      domain: domainName,
      registeredAt: str(domainRecord, "registered_at"),
      registrar: str(domainRecord, "registrar"),
      unregistered: bool(domainRecord, "unregistered"),
    };
  }

  let exposure: EvidenceExposure | undefined;
  const exposureRecord = asRecord(root["exposure"]);
  // A status other than seen/unseen is a document this parser does not
  // understand; dropping the section renders "could not be gathered" — the
  // conservative unknown — rather than a reassuring no-traffic state.
  const exposureStatus = exposureRecord
    ? str(exposureRecord, "status")
    : undefined;
  if (
    exposureRecord &&
    (exposureStatus === "seen" || exposureStatus === "unseen")
  ) {
    exposure = {
      status: exposureStatus,
      canonicalUrl: str(exposureRecord, "canonical_url"),
      urlHost: str(exposureRecord, "url_host"),
      serverName: str(exposureRecord, "server_name"),
      firstSeen: str(exposureRecord, "first_seen"),
      lastSeen: str(exposureRecord, "last_seen"),
      firstCalled: str(exposureRecord, "first_called"),
      lastCalled: str(exposureRecord, "last_called"),
      callCount: num(exposureRecord, "call_count"),
      userCount: num(exposureRecord, "user_count"),
      inUse: bool(exposureRecord, "in_use"),
    };
  }

  let authority: EvidenceAuthority | undefined;
  const authorityRecord = asRecord(root["authority"]);
  if (authorityRecord) {
    authority = {
      mode: str(authorityRecord, "mode") ?? "undeclared",
      transport: str(authorityRecord, "transport"),
      scopes: strings(authorityRecord["scopes"]),
      dynamicRegistration: bool(authorityRecord, "dynamic_registration"),
      demandedSecrets: credentials(authorityRecord["demanded_secrets"]),
      optionalSecrets: credentials(authorityRecord["optional_secrets"]),
      unauthenticatedTools: strings(authorityRecord["unauthenticated_tools"]),
      undeclared: bool(authorityRecord, "undeclared"),
    };
  }

  const capabilities: EvidenceCapability[] = [];
  const rawCapabilities = root["capabilities"];
  if (Array.isArray(rawCapabilities)) {
    for (const entry of rawCapabilities) {
      const record = asRecord(entry);
      const tool = record ? str(record, "tool") : undefined;
      if (!record || !tool) continue;
      capabilities.push({
        tool,
        declared: strings(record["declared"]),
        schemaImplied: strings(record["schema_implied"]),
        actsOnBehalf: bool(record, "acts_on_behalf"),
        unannotated: bool(record, "unannotated"),
      });
    }
  }

  let provenance: EvidenceProvenance | undefined;
  const provenanceRecord = asRecord(root["provenance"]);
  if (provenanceRecord) {
    provenance = {
      registry: str(provenanceRecord, "registry"),
      specifier: str(provenanceRecord, "specifier"),
      catalogued: bool(provenanceRecord, "catalogued"),
      official: bool(provenanceRecord, "official"),
      status: str(provenanceRecord, "status"),
      isLatest: bool(provenanceRecord, "is_latest"),
      publishedAt: str(provenanceRecord, "published_at"),
      updatedAt: str(provenanceRecord, "updated_at"),
      visitorsLastWeek: num(provenanceRecord, "visitors_last_week"),
      visitorsLastFourWeeks: num(provenanceRecord, "visitors_last_four_weeks"),
      visitorsTotal: num(provenanceRecord, "visitors_total"),
    };
  }

  return {
    identity,
    package: packageSection,
    packageNotPublished: bool(root, "package_not_published"),
    repository,
    repositoryNotFound: bool(root, "repository_not_found"),
    advisories: advisoriesSection,
    domain,
    exposure,
    authority,
    capabilities,
    capabilitiesSource: capabilitiesSource(str(root, "capabilities_source")),
    provenance,
    gaps: strings(root["gaps"]),
  };
}

function capabilitiesSource(
  value: string | undefined,
): "server" | "registry" | undefined {
  if (value === "server" || value === "registry") return value;
  return undefined;
}

function strings(value: unknown): string[] {
  if (!Array.isArray(value)) return [];
  return value.filter((entry): entry is string => typeof entry === "string");
}

function advisoryItems(value: unknown): EvidenceAdvisoryItem[] {
  if (!Array.isArray(value)) return [];
  const out: EvidenceAdvisoryItem[] = [];
  for (const entry of value) {
    const record = asRecord(entry);
    const id = record ? str(record, "id") : undefined;
    if (!record || !id) continue;
    out.push({
      id,
      summary: str(record, "summary"),
      severity: str(record, "severity"),
      published: str(record, "published"),
    });
  }
  return out;
}

function credentials(value: unknown): EvidenceCredential[] {
  if (!Array.isArray(value)) return [];
  const out: EvidenceCredential[] = [];
  for (const entry of value) {
    const record = asRecord(entry);
    const name = record ? str(record, "name") : undefined;
    if (!record || !name) continue;
    out.push({
      name,
      required: bool(record, "required"),
      description: str(record, "description"),
    });
  }
  return out;
}
