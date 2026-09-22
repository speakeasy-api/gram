/**
 * Reading the evidence document as a ranked list of findings.
 *
 * The evidence panel answers six questions, and every answer that mattered
 * used to be a small bordered aside inside whichever question happened to
 * raise it — eleven possible sites, in reading order rather than in order of
 * consequence. A reviewer looking at a server with sixty-one tools had to
 * read all of it to discover that thirty-four of them declare they act on
 * your behalf.
 *
 * This module does that reading once. It derives every finding the document
 * supports, ranks them, and hands the panel a list it can put above the
 * evidence: what stands out, then the facts behind it. Nothing here is new
 * information — each signal points at the section that shows its working.
 *
 * Two rules it must not break, inherited from the panel it summarizes:
 *
 *  - A failed lookup is a finding. It ranks as `unknown`, never as absent.
 *  - A clean answer is a fact about a check, not a verdict about the server.
 *    Clean signals say which check ran and what it returned, and they are
 *    ranked last and rendered without a safe color.
 */

import { gapLabel, type EvidenceDocument } from "./evidence";

/**
 * How much a finding should interrupt the reader.
 *
 * `concern` is for a published fact that would justify a denial on its own.
 * `watch` is for authority the server asks for or holds — true of plenty of
 * legitimate servers, and the substance of most reviews. `unknown` is a
 * question the gather could not answer. `clean` is a check that ran and
 * returned nothing.
 */
export type SignalTone = "concern" | "watch" | "unknown" | "clean";

/** The evidence section that shows a signal's working. */
export type EvidenceSectionId =
  | "trust"
  | "handover"
  | "capabilities"
  | "usage"
  | "maturity"
  | "advisories";

/**
 * The anchor id an evidence section registers so a signal can jump to it. One
 * function, so the link and its target cannot drift apart.
 */
export function evidenceSectionAnchor(section: EvidenceSectionId): string {
  return `evidence-${section}`;
}

export type EvidenceSignal = {
  /** Stable across re-gathers, so React keys and tests can rely on it. */
  id: string;
  tone: SignalTone;
  /** The finding itself, as one sentence a reviewer can act on. */
  headline: string;
  /** What qualifies it: where it came from, or what it does not prove. */
  detail?: string;
  /**
   * Absent when no section shows more than the headline already says — a
   * failed lookup has no facts to jump to.
   */
  section?: EvidenceSectionId;
};

const TONE_RANK: Record<SignalTone, number> = {
  concern: 0,
  watch: 1,
  unknown: 2,
  clean: 3,
};

const DAY_MS = 24 * 60 * 60 * 1000;

/** Under this, a domain is young enough to be worth saying out loud. */
const YOUNG_DOMAIN_DAYS = 180;

/** Over this, the domain's age is worth recording as a check that ran. */
const ESTABLISHED_DOMAIN_DAYS = 2 * 365;

function daysSince(iso: string): number | null {
  const parsed = Date.parse(iso);
  if (Number.isNaN(parsed)) return null;
  return Math.floor((Date.now() - parsed) / DAY_MS);
}

function plural(count: number, one: string, many: string): string {
  return count === 1 ? one : many;
}

/** "34 of 61 tools" — the ratio, because the denominator is the whole point. */
function ofTools(count: number, total: number): string {
  return `${count} of ${total} ${plural(total, "tool", "tools")}`;
}

/**
 * Tools whose declarations or input schemas include a given capability.
 * `declared` is what the server annotated; `schemaImplied` is what its input
 * schema gives away regardless of annotation.
 */
function toolsWith(document: EvidenceDocument, capability: string): number {
  return document.capabilities.filter(
    (tool) =>
      tool.declared.includes(capability) ||
      tool.schemaImplied.includes(capability),
  ).length;
}

/**
 * Input-schema capabilities worth calling out on their own. These are not
 * annotations the server chose to publish — they are what its parameters
 * accept, which is why they are listed separately from `acts on your behalf`.
 */
const SCHEMA_SIGNALS: Array<{
  capability: string;
  /** What the parameter is, as the object of "takes a …". */
  takes: string;
  detail: string;
}> = [
  {
    capability: "arbitrary_command",
    takes: "shell command",
    detail:
      "Whatever the model puts in that field runs wherever the server runs it.",
  },
  {
    capability: "credential_input",
    takes: "credential",
    detail:
      "Secrets passed as tool arguments travel through the model's context.",
  },
  {
    capability: "filesystem_path",
    takes: "filesystem path",
    detail: "The schema constrains it to no particular directory.",
  },
  {
    capability: "arbitrary_url",
    takes: "URL",
    detail:
      "A tool that fetches a caller-supplied URL can be pointed at internal hosts.",
  },
];

/**
 * Every finding the document supports, most consequential first.
 *
 * Pure and synchronous: the panel calls it during render, and the tests call
 * it directly. Given a null document — nothing gathered — it returns nothing,
 * because a page with no evidence has no findings to rank, only a gap to
 * report, and the panel says that in its own words.
 */
export function evidenceSignals(
  document: EvidenceDocument | null,
): EvidenceSignal[] {
  if (!document) return [];

  const signals: EvidenceSignal[] = [];
  const add = (signal: EvidenceSignal): void => {
    signals.push(signal);
  };

  collectIdentitySignals(document, add);
  collectAuthoritySignals(document, add);
  collectCapabilitySignals(document, add);
  collectMaturitySignals(document, add);
  collectAdvisorySignals(document, add);
  collectExposureSignals(document, add);
  collectGapSignals(document, add);

  // A stable sort, so the declaration order above is the tiebreak within a
  // tone: the order each collector writes its signals in is deliberate.
  return signals.sort(
    (left, right) => TONE_RANK[left.tone] - TONE_RANK[right.tone],
  );
}

type Emit = (signal: EvidenceSignal) => void;

function collectIdentitySignals(document: EvidenceDocument, add: Emit): void {
  const { identity, domain } = document;

  if (identity.kind === "unresolved") {
    add({
      id: "identity-unresolved",
      tone: "concern",
      headline: "This reference resolves to no identifiable server",
      detail: "Who publishes or operates it cannot be established at all.",
      section: "trust",
    });
    return;
  }

  if (domain?.unregistered) {
    add({
      id: "domain-unregistered",
      tone: "concern",
      headline: `The domain registry has no registration for ${domain.domain}`,
      detail: "Unusual for a host that answers traffic.",
      section: "trust",
    });
  }

  if (identity.kind === "remote" && !identity.registrableDomain) {
    add({
      id: "no-registrable-domain",
      tone: "watch",
      headline: "The host has no registrable public domain",
      detail: "Nothing links it to a publisher anyone could be accountable to.",
      section: "trust",
    });
  }

  // Only a package can pin a version. A remote endpoint serves whatever it
  // serves, so saying "no version is pinned" about one is noise dressed as a
  // finding — it was on the page for every remote server ever reviewed.
  if (identity.kind === "package" && !identity.versionPinned) {
    add({
      id: "version-unpinned",
      tone: "watch",
      headline: "No version is pinned",
      detail:
        "The evidence below describes the version published today; installs resolve to whatever is latest at the time.",
      section: "trust",
    });
  }

  if (document.package?.maintainerCount === 1) {
    add({
      id: "single-maintainer",
      tone: "watch",
      headline: "One registry account can publish this package",
      detail:
        "A single compromised maintainer account reaches everyone who installs it.",
      section: "trust",
    });
  }

  const registeredDays = domain?.registeredAt
    ? daysSince(domain.registeredAt)
    : null;
  if (registeredDays !== null && registeredDays >= 0) {
    if (registeredDays < YOUNG_DOMAIN_DAYS) {
      add({
        id: "domain-young",
        tone: "watch",
        headline: `The domain was registered ${registeredDays} ${plural(registeredDays, "day", "days")} ago`,
        detail:
          "Recent registration is common to both new products and throwaway hosts.",
        section: "trust",
      });
    } else if (registeredDays > ESTABLISHED_DOMAIN_DAYS) {
      const years = Math.floor(registeredDays / 365);
      add({
        id: "domain-established",
        tone: "clean",
        headline: `The domain has been registered for ${years} ${plural(years, "year", "years")}`,
        detail: "Registration age, not ownership — domains change hands.",
        section: "trust",
      });
    }
  }
}

function collectAuthoritySignals(document: EvidenceDocument, add: Emit): void {
  const { authority } = document;

  if (!authority || authority.undeclared) {
    // A probe that failed is already reported as a gap, in words that say the
    // lookup broke. Saying "the server publishes no metadata" beside it would
    // credit the server with an answer it never gave.
    if (!document.gaps.includes("authority_probe_failed")) {
      add({
        id: "authority-undeclared",
        tone: "unknown",
        headline: "The server publishes no authentication metadata",
        detail:
          "What it will ask a user to hand over is unknown — which is not the same as nothing.",
        section: "handover",
      });
    }
    return;
  }

  if (authority.unauthenticatedTools.length > 0) {
    add({
      id: "unauthenticated-listing",
      tone: "concern",
      headline: `It listed ${authority.unauthenticatedTools.length} ${plural(authority.unauthenticatedTools.length, "tool", "tools")} to a caller holding no credential`,
      detail: "The protocol answered without any authentication at all.",
      section: "handover",
    });
  }

  if (authority.demandedSecrets.length > 0) {
    const names = authority.demandedSecrets
      .map((secret) => secret.name)
      .join(", ");
    add({
      id: "demands-secret",
      tone: "concern",
      headline: `It requires a static secret: ${names}`,
      detail:
        "A pasted secret is not scoped to a user and cannot be revoked for one.",
      section: "handover",
    });
  } else if (authority.mode === "api_key") {
    add({
      id: "api-key-auth",
      tone: "watch",
      headline: "It authenticates with a static secret pasted at install",
      detail: "Not scoped per user, and revoking it revokes it for everyone.",
      section: "handover",
    });
  }

  if (authority.dynamicRegistration) {
    add({
      id: "dynamic-registration",
      tone: "watch",
      headline: "It publishes dynamic client registration",
      detail:
        "Any client can register itself with the authorization server without an administrator.",
      section: "handover",
    });
  }

  const writeScopes = authority.scopes.filter(isWriteScope);
  if (writeScopes.length > 0) {
    add({
      id: "write-scopes",
      tone: "watch",
      headline: `It will ask to be granted ${writeScopes.join(", ")}`,
      detail:
        "These are the scopes the authorization server actually enforces, unlike the tool annotations.",
      section: "handover",
    });
  }

  if (authority.mode === "oauth") {
    add({
      id: "oauth-auth",
      tone: "clean",
      headline: "Access is delegated through OAuth",
      detail: "Scoped and revocable per user — the mechanism, not the scope.",
      section: "handover",
    });
  }
}

/**
 * Scope strings whose name says they grant more than reading. Deliberately
 * crude: scope vocabularies are per-vendor, so this recognizes the common
 * words and stays silent otherwise rather than guessing at a taxonomy.
 */
function isWriteScope(scope: string): boolean {
  return /(^|[.:_\-/])(write|admin|delete|manage|all)([.:_\-/]|$)/i.test(scope);
}

function collectCapabilitySignals(document: EvidenceDocument, add: Emit): void {
  const total = document.capabilities.length;
  if (total === 0) {
    if (document.capabilitiesSource) {
      add({
        id: "tools-declared-empty",
        tone: "clean",
        headline:
          document.capabilitiesSource === "registry"
            ? "The registry catalog's copy declares no tools"
            : "The server answered the tool listing with zero tools",
        detail: "The listing succeeded; this is a declared-empty toolset.",
        section: "capabilities",
      });
    }
    return;
  }

  const actingCount = document.capabilities.filter(
    (tool) => tool.actsOnBehalf,
  ).length;
  if (actingCount > 0) {
    add({
      id: "acts-on-behalf",
      tone: "watch",
      headline: `${ofTools(actingCount, total)} ${plural(actingCount, "declares", "declare")} it acts on your behalf`,
      detail: "Each one does more than read when the model calls it.",
      section: "capabilities",
    });
  }

  const destructiveCount = toolsWith(document, "destructive");
  if (destructiveCount > 0) {
    add({
      id: "destructive-tools",
      tone: "watch",
      headline: `${ofTools(destructiveCount, total)} ${plural(destructiveCount, "declares", "declare")} destructive effects`,
      detail: "The server's own annotation: these are not reversible by it.",
      section: "capabilities",
    });
  }

  for (const schema of SCHEMA_SIGNALS) {
    const count = toolsWith(document, schema.capability);
    if (count === 0) continue;
    add({
      id: `schema-${schema.capability}`,
      tone: "watch",
      headline: `${ofTools(count, total)} ${plural(count, "takes", "take")} a ${schema.takes} as a parameter`,
      detail: schema.detail,
      section: "capabilities",
    });
  }

  const unannotatedCount = document.capabilities.filter(
    (tool) => tool.unannotated,
  ).length;
  if (unannotatedCount > 0) {
    add({
      id: "unannotated-tools",
      tone: "unknown",
      headline: `${ofTools(unannotatedCount, total)} ${plural(unannotatedCount, "declares", "declare")} nothing about what ${plural(unannotatedCount, "it does", "they do")}`,
      detail:
        "No annotation is not a claim to be harmless — their authority is unknown.",
      section: "capabilities",
    });
  }

  if (document.capabilitiesSource === "registry") {
    add({
      id: "capabilities-from-registry",
      tone: "unknown",
      headline: "The tool list came from a registry catalog, not the server",
      detail:
        "The server itself did not answer without credentials, so what it serves today may differ.",
      section: "capabilities",
    });
  }
}

function collectMaturitySignals(document: EvidenceDocument, add: Emit): void {
  if (document.packageNotPublished) {
    add({
      id: "package-not-published",
      tone: "concern",
      headline: "The registry has no package by this name",
      detail:
        "This reference points at something its own registry cannot serve.",
      section: "maturity",
    });
  }

  if (document.package?.deprecated) {
    add({
      id: "package-deprecated",
      tone: "concern",
      headline: "The registry marks the current version deprecated",
      detail: document.package.deprecationReason,
      section: "maturity",
    });
  }

  if (document.repositoryNotFound) {
    add({
      id: "repository-missing",
      tone: "concern",
      headline: "The declared source repository does not exist",
      detail:
        "The publisher names a repository the code host has never had — the artifact traces to no source.",
      section: "maturity",
    });
  }

  if (document.repository?.archived) {
    add({
      id: "repository-archived",
      tone: "watch",
      headline: "The declared source repository is archived",
      detail: "Its owner froze it against further commits and issues.",
      section: "maturity",
    });
  }

  if (document.provenance?.catalogued && document.provenance.official) {
    add({
      id: "catalogued-official",
      tone: "clean",
      headline: `${document.provenance.registry ?? "An MCP registry"} vouches for the publisher`,
      detail:
        "The catalog's claim about who publishes it, not about how it behaves.",
      section: "maturity",
    });
  }
}

function collectAdvisorySignals(document: EvidenceDocument, add: Emit): void {
  // Advisory databases index published packages. For a remote endpoint the
  // question is real and the database simply cannot answer it, so it ranks as
  // an unknown rather than occupying a whole section that always reads empty.
  if (document.identity.kind !== "package") {
    add({
      id: "advisories-not-applicable",
      tone: "unknown",
      headline: "No advisory database covers a hosted endpoint",
      detail:
        "Published vulnerabilities are indexed against packages. This server's security history is a research question.",
    });
    return;
  }

  const { advisories } = document;
  if (!advisories) return;

  if (advisories.knownCount > 0) {
    add({
      id: "advisories-found",
      tone: "concern",
      headline: `${advisories.knownCount} published ${plural(advisories.knownCount, "advisory names", "advisories name")} this package`,
      section: "advisories",
    });
    return;
  }

  add({
    id: "advisories-clean",
    tone: "clean",
    headline: "OSV lists no published advisories for this package",
    detail: "Checked today; it says nothing about unreported issues.",
    section: "advisories",
  });
}

function collectExposureSignals(document: EvidenceDocument, add: Emit): void {
  const { exposure } = document;
  if (!exposure) return;

  if (exposure.status === "unseen") {
    add({
      id: "exposure-unseen",
      tone: "clean",
      headline: "Nobody here has called it",
      detail: "Denying it costs no one an existing workflow.",
      section: "usage",
    });
    return;
  }

  if (!exposure.inUse) return;

  const people = exposure.userCount ?? 0;
  add({
    id: "exposure-in-use",
    tone: "watch",
    headline:
      people > 0
        ? `Already in use — denying it changes ${people} ${plural(people, "person's", "people's")} workflow`
        : "Already in use — denying it changes existing workflows",
    detail:
      exposure.callCount !== undefined
        ? `${exposure.callCount.toLocaleString()} recorded ${plural(exposure.callCount, "call", "calls")}.`
        : undefined,
    section: "usage",
  });
}

function collectGapSignals(document: EvidenceDocument, add: Emit): void {
  for (const gap of document.gaps) {
    add({
      id: `gap-${gap}`,
      tone: "unknown",
      headline: gapLabel(gap),
      detail: "Treat it as unknown, not as clean.",
    });
  }
}

/** How many of each tone are in a list, for the panel's header count. */
export function countByTone(
  signals: EvidenceSignal[],
): Record<SignalTone, number> {
  const counts: Record<SignalTone, number> = {
    concern: 0,
    watch: 0,
    unknown: 0,
    clean: 0,
  };
  for (const signal of signals) counts[signal.tone] += 1;
  return counts;
}
