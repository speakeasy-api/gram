import {
  EvidenceGroup,
  EvidencePanel,
  StatusBadge,
} from "@/components/mcp-approvals/EvidencePanel";
import { EvidenceChangedNotice } from "@/components/mcp-approvals/EvidenceChangedNotice";
import {
  parseEvidenceDocument,
  USAGE_QUESTION,
} from "@/components/mcp-approvals/evidence";
import { Button } from "@/components/ui/Button";
import { Heading } from "@/components/ui/Heading";
import { Text } from "@/components/ui/Text";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { useProject } from "@/contexts/Auth";
import { HumanizeDateTime } from "@/lib/dates";
import {
  openSafeExternalUrl,
  safeExternalHttpUrl,
} from "@/lib/safe-external-url";
import { cn } from "@/lib/utils";
import type { ApprovalDecision } from "@gram/client/models/components/approvaldecision.js";
import type { ApprovalRequester } from "@gram/client/models/components/approvalrequester.js";
import type { ResearchReport } from "@gram/client/models/components/researchreport.js";
import {
  invalidateGetMcpApprovalRequest,
  useGetMcpApprovalRequest,
} from "@gram/client/react-query/getMcpApprovalRequest.js";
import { invalidateAllListMcpApprovalRequests } from "@gram/client/react-query/listMcpApprovalRequests.js";
import { useRefreshMcpApprovalEvidenceMutation } from "@gram/client/react-query/refreshMcpApprovalEvidence.js";
import { useStartMcpResearchMutation } from "@gram/client/react-query/startMcpResearch.js";
import { RequireScope } from "@/components/require-scope";
import { useQueryClient } from "@tanstack/react-query";
import { ChevronDown, ChevronUp, Loader2, RefreshCw } from "lucide-react";
import { useMemo, useState } from "react";
import { toast } from "sonner";

/**
 * One approval review: the evidence, everyone who asked, and every prior
 * decision. Rendered wherever a server is reviewed — the Shadow MCP server
 * page for URL targets, the queue's review sheet for stdio targets. Its job
 * is to make one decision fast and defensible, and never to let an absence
 * of evidence read as evidence of safety. Deciding always happens in the
 * Decide Access sheet, never inline here — one write path, one form.
 */
export function ApprovalReview({
  requestId,
  title,
  description,
  usage,
}: {
  requestId: string;
  /**
   * Section title, when this review is a section of a larger page. Given
   * one, the request's status shares its row instead of costing a strip of
   * its own; the review sheet, which has its own title, omits it.
   */
  title?: string;
  description?: string;
  /**
   * Who is calling the server today. Owned by the caller, which holds the
   * traffic query, and rendered inside the evidence as one more question
   * about the server.
   */
  usage?: React.ReactNode;
}): JSX.Element {
  const project = useProject();

  const detailQuery = useGetMcpApprovalRequest(
    { id: requestId, gramProject: project.slug },
    undefined,
    {
      enabled: requestId.length > 0 && project.slug.length > 0,
      // A running research report resolves server-side; poll until it does
      // so the report appears without a manual refresh.
      refetchInterval: (query) =>
        query.state.data?.researchReports.some(
          (report) => report.status === "running",
        )
          ? 5_000
          : false,
    },
  );

  const detail = detailQuery.data;
  const document = useMemo(
    () => parseEvidenceDocument(detail?.evidence, detail?.evidenceVersion),
    [detail],
  );

  // A failed fetch must not read as "still loading" forever: name the
  // failure and offer a retry.
  // Observed traffic comes from the caller's own query, so it survives a
  // review that will not load: what the server is doing right now is exactly
  // what an admin still wants when the dossier is unavailable.
  if (detailQuery.error && !detail) {
    return (
      <div className="space-y-4">
        <div className="bg-muted/20 flex min-h-24 flex-col items-center justify-center border border-dashed px-6 py-8 text-center">
          <p className="text-sm font-medium">The review could not be loaded</p>
          <p className="text-muted-foreground mt-1 max-w-md text-sm">
            It may be a temporary problem — try again.
          </p>
          <Button
            className="mt-3"
            variant="secondary"
            onClick={() => void detailQuery.refetch()}
          >
            <Button.Text>Retry</Button.Text>
          </Button>
        </div>
        {usage && (
          <EvidenceGroup question={USAGE_QUESTION}>{usage}</EvidenceGroup>
        )}
      </div>
    );
  }

  if (!detail) {
    return (
      <div className="space-y-4">
        <SkeletonTable />
        {usage && (
          <EvidenceGroup question={USAGE_QUESTION}>{usage}</EvidenceGroup>
        )}
      </div>
    );
  }

  const unreviewed = detail.request.status === "unreviewed";

  return (
    <div className="space-y-4">
      {/* An unreviewed dossier is evidence without a review: no status to
          report, nobody waiting. The requester list and decision history only
          exist once someone actually asks or decides. */}
      <ReviewHeader
        title={title}
        description={description}
        status={detail.request.status}
        createdAt={unreviewed ? undefined : detail.request.createdAt}
        versionPinned={detail.request.versionPinned}
      />
      {detail.request.status === "approved" && detail.evidenceDiff?.changed && (
        <EvidenceChangedNotice
          diff={detail.evidenceDiff}
          changedAt={detail.request.evidenceChangedAt}
        />
      )}
      {unreviewed ? (
        <>
          <p className="text-muted-foreground text-sm">
            No one has asked for this server and no decision has been recorded.
          </p>
          <PriorDecisions decisions={detail.decisions} />
        </>
      ) : (
        <>
          {/* Who asked and what we last decided are both short lists read
              before the evidence, so they sit side by side rather than
              pushing the evidence a screen further down. */}
          <div className="grid items-start gap-x-6 gap-y-4 md:grid-cols-2">
            <Requesters requesters={detail.requesters} />
            <PriorDecisions decisions={detail.decisions} />
          </div>
        </>
      )}
      <EvidencePanel
        document={document}
        collectedAt={detail.evidenceCollectedAt}
        usage={usage}
      />
      <ResearchReports reports={detail.researchReports} requestId={requestId} />
    </div>
  );
}

/**
 * Re-runs every evidence source and swaps in the fresh gather. Decisions
 * freeze their own snapshots, so refreshing never rewrites what a prior
 * reviewer saw.
 */
export function RefreshEvidenceButton({
  requestId,
  projectSlug,
  ready,
}: {
  requestId: string;
  projectSlug: string;
  ready: boolean;
}): JSX.Element {
  const queryClient = useQueryClient();
  const refresh = useRefreshMcpApprovalEvidenceMutation();

  const run = async () => {
    try {
      await refresh.mutateAsync({
        request: { id: requestId, gramProject: projectSlug },
      });
    } catch {
      toast.error("Evidence refresh failed — the stored evidence is unchanged");
      return;
    }
    await Promise.all([
      invalidateGetMcpApprovalRequest(queryClient, [{ id: requestId }]),
      invalidateAllListMcpApprovalRequests(queryClient),
    ]);
    toast.success("Evidence re-gathered");
  };

  return (
    <Button
      variant="secondary"
      size="sm"
      onClick={() => void run()}
      disabled={!ready || refresh.isPending}
    >
      <Button.LeftIcon>
        <RefreshCw className={refresh.isPending ? "animate-spin" : undefined} />
      </Button.LeftIcon>
      <Button.Text>
        {refresh.isPending ? "Refreshing" : "Refresh Data"}
      </Button.Text>
    </Button>
  );
}

/**
 * The section title and the request's standing on one row. The status was a
 * bordered card of its own until the labels came off it and nothing was left
 * that the badge and the date did not already say — so it rides along in the
 * space beside the heading instead of costing a band of its own.
 */
function ReviewHeader({
  title,
  description,
  status,
  createdAt,
  versionPinned,
}: {
  title: string | undefined;
  description: string | undefined;
  status: string;
  /** Absent for an unreviewed dossier: nobody raised it. */
  createdAt: Date | undefined;
  versionPinned: boolean;
}): JSX.Element {
  return (
    <div className="flex flex-wrap items-start justify-between gap-x-6 gap-y-1">
      {title && (
        <div className="min-w-0 flex-1">
          <Text variant="subheading">{title}</Text>
          {description && (
            <Text muted small>
              {description}
            </Text>
          )}
        </div>
      )}
      <div className="flex shrink-0 flex-wrap items-center gap-x-3 gap-y-1 text-xs">
        <StatusBadge status={status} />
        {createdAt && (
          <span className="text-muted-foreground">
            First raised{" "}
            <HumanizeDateTime date={createdAt} includeTime={false} />
          </span>
        )}
        {!versionPinned && (
          <span className="text-muted-foreground">
            No pinned version — what runs may differ from the evidence.
          </span>
        )}
      </div>
    </div>
  );
}

function Requesters({
  requesters,
}: {
  requesters: ApprovalRequester[];
}): JSX.Element {
  return (
    <section className="space-y-2">
      <h3 className="text-eyebrow">Who asked, and why</h3>
      {requesters.length === 0 ? (
        <p className="text-muted-foreground text-sm">
          No requester is attached — this ask arrived without an identifiable
          user.
        </p>
      ) : (
        // One framed list with hairline rules, not a stack of cards: three
        // requesters cost three rows here instead of three bordered blocks
        // with gaps between them.
        <ul className="divide-border border-border divide-y border">
          {requesters.map((requester) => (
            <li key={requester.userId} className="px-3 py-1.5 text-xs">
              <div className="flex items-baseline justify-between gap-2">
                <span className="truncate font-medium">
                  {requester.userEmail ?? requester.userId}
                </span>
                <span className="text-muted-foreground shrink-0 text-xs">
                  <HumanizeDateTime
                    date={requester.requestedAt}
                    includeTime={false}
                  />
                </span>
              </div>
              {requester.note && (
                <p className="text-muted-foreground mt-0.5">
                  "{requester.note}"
                </p>
              )}
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

function PriorDecisions({
  decisions,
}: {
  decisions: ApprovalDecision[];
}): JSX.Element {
  return (
    <section className="space-y-2">
      <h3 className="text-eyebrow">Have we decided on this before?</h3>
      {decisions.length === 0 ? (
        <p className="text-muted-foreground text-sm">
          No prior decisions. This is the first review of this server here.
        </p>
      ) : (
        <ul className="divide-border border-border divide-y border">
          {decisions.map((decision) => (
            <li key={decision.id} className="px-3 py-1.5 text-xs">
              <div className="flex items-center justify-between gap-2">
                <StatusBadge status={decision.decision} />
                <span className="text-muted-foreground shrink-0 text-xs">
                  <HumanizeDateTime
                    date={decision.decidedAt}
                    includeTime={false}
                  />
                </span>
              </div>
              {decision.rationale && (
                <p className="text-muted-foreground mt-1">
                  "{decision.rationale}"
                </p>
              )}
              <FrozenEvidence decision={decision} />
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

/**
 * The evidence exactly as it stood when a decision was made. Frozen at
 * decision time — a later re-gather updates the request's evidence but never
 * this snapshot, so what the reviewer actually saw stays inspectable.
 */
function FrozenEvidence({
  decision,
}: {
  decision: ApprovalDecision;
}): JSX.Element {
  const [open, setOpen] = useState(false);
  const document = useMemo(
    () => parseEvidenceDocument(decision.evidence, decision.evidenceVersion),
    [decision],
  );

  if (decision.evidence === undefined || decision.evidence === null) {
    return (
      <p className="text-muted-foreground mt-1 text-xs">
        No evidence was on file when this was decided.
      </p>
    );
  }

  return (
    <div className="mt-1.5">
      <button
        type="button"
        onClick={() => setOpen((current) => !current)}
        className="text-muted-foreground hover:text-foreground flex items-center gap-1 text-xs"
      >
        {open ? (
          <ChevronUp className="size-3" />
        ) : (
          <ChevronDown className="size-3" />
        )}
        {open ? "Hide" : "View"} the evidence as it stood then
        {decision.evidenceVersion !== undefined &&
          ` (v${decision.evidenceVersion})`}
      </button>
      {open && (
        <div className="border-border mt-2 border-l pl-2.5">
          <EvidencePanel document={document} collectedAt={undefined} />
        </div>
      )}
    </div>
  );
}

function reportStatusLabel(status: string): string {
  switch (status) {
    case "running":
      return "Running";
    case "completed":
      return "Completed";
    case "failed":
      return "Failed";
    default:
      return status;
  }
}

function ResearchReports({
  reports,
  requestId,
}: {
  reports: ResearchReport[];
  requestId: string;
}): JSX.Element {
  const project = useProject();
  const queryClient = useQueryClient();
  const startResearch = useStartMcpResearchMutation({
    onSuccess: async () => {
      await invalidateGetMcpApprovalRequest(queryClient, [
        { id: requestId, gramProject: project.slug },
      ]);
    },
    onError: () => {
      toast.error("Starting the research run failed — nothing was spent");
    },
  });

  const [showPrevious, setShowPrevious] = useState(false);
  const running = reports.some((report) => report.status === "running");

  // Newest first from the API: the latest run is the report; earlier runs
  // are history behind a toggle.
  const latest = reports[0];
  const previous = reports.slice(1);

  return (
    <section className="space-y-2">
      <div className="flex items-center justify-between gap-2">
        <Heading variant="h3" className="text-lg font-thin">
          Web research
        </Heading>
        {/* The same org-admin gate as the endpoint behind it: research
            spends the org's money, so a non-admin must not see a button
            whose click comes back 403. */}
        <RequireScope scope="org:admin" level="component">
          <Button
            size="sm"
            variant="secondary"
            disabled={running || startResearch.isPending}
            onClick={() =>
              startResearch.mutate({
                request: { id: requestId, gramProject: project.slug },
              })
            }
          >
            {running && (
              <Button.LeftIcon>
                <Loader2 className="animate-spin" />
              </Button.LeftIcon>
            )}
            <Button.Text>
              {running ? "Researching…" : "Run Research"}
            </Button.Text>
          </Button>
        </RequireScope>
      </div>
      <p className="text-muted-foreground text-xs">
        Gathered by an agent from public web sources, which may be inaccurate,
        incomplete, or deliberately seeded. Read them as leads to verify, not as
        findings — the agent gathers and cites, it never decides.
      </p>
      <p className="border-warning border px-2.5 py-1.5 text-xs">
        <span className="font-medium">
          A research run spends real AI credits.
        </span>{" "}
        The agent makes dozens of model calls, web searches, and page reads —
        typically several hundred thousand tokens, several minutes per run.
      </p>
      {!latest && (
        <p className="border-border text-muted-foreground border border-dashed px-2.5 py-1.5 text-xs">
          No research has been run for this server.
        </p>
      )}
      {latest && <ResearchReportCard report={latest} />}
      {previous.length > 0 && (
        <>
          <button
            type="button"
            aria-expanded={showPrevious}
            onClick={() => setShowPrevious((current) => !current)}
            className="text-muted-foreground hover:text-foreground flex items-center gap-1 text-xs"
          >
            {showPrevious ? (
              <>
                Hide previous runs
                <ChevronUp className="size-3" />
              </>
            ) : (
              <>
                Show {previous.length} previous{" "}
                {previous.length === 1 ? "run" : "runs"}
                <ChevronDown className="size-3" />
              </>
            )}
          </button>
          {showPrevious && (
            <ul className="space-y-3">
              {previous.map((report) => (
                <li key={report.id}>
                  <ResearchReportCard report={report} />
                </li>
              ))}
            </ul>
          )}
        </>
      )}
    </section>
  );
}

function ResearchReportCard({
  report,
}: {
  report: ResearchReport;
}): JSX.Element {
  return (
    <div className="border-border border p-2.5 text-xs">
      <div className="flex items-center justify-between gap-2">
        <span className="flex items-center gap-1.5 font-medium">
          {report.status === "running" && (
            <Loader2 className="size-3 animate-spin" />
          )}
          {reportStatusLabel(report.status)}
        </span>
        <span className="text-muted-foreground shrink-0 text-xs">
          <HumanizeDateTime date={report.createdAt} />
        </span>
      </div>
      {report.status === "running" && (
        <p className="text-muted-foreground mt-1">
          The agent is searching and reading — this usually takes a few minutes.
        </p>
      )}
      {report.error && (
        <p className="text-muted-foreground mt-1">{report.error}</p>
      )}
      <ReportBody report={report.report} />
      <ReportRunMeta report={report} />
    </div>
  );
}

/**
 * The report's summary and coverage, rendered above the claims. Thin or
 * absent coverage is the headline finding, so it renders as a bordered
 * callout rather than fine print.
 */
function ReportBody({ report }: { report: unknown }): JSX.Element | null {
  if (typeof report !== "object" || report === null || Array.isArray(report)) {
    return null;
  }
  const record = report as Record<string, unknown>;
  const summary =
    typeof record["summary"] === "string" ? record["summary"] : "";
  const coverage =
    typeof record["coverage"] === "object" &&
    record["coverage"] !== null &&
    !Array.isArray(record["coverage"])
      ? (record["coverage"] as Record<string, unknown>)
      : null;
  const coverageLevel =
    coverage && typeof coverage["level"] === "string" ? coverage["level"] : "";
  const coverageNote =
    coverage && typeof coverage["note"] === "string" ? coverage["note"] : "";

  return (
    <>
      <ReportInjections report={report} />
      {summary && <p className="mt-2">{summary}</p>}
      {(coverageLevel === "none" || coverageLevel === "thin") && (
        <p className="border-warning mt-2 border px-2.5 py-1.5">
          <span className="font-medium">
            {coverageLevel === "none"
              ? "No independent coverage exists."
              : "Independent coverage is thin."}
          </span>
          {coverageNote && (
            <span className="text-muted-foreground"> {coverageNote}</span>
          )}
        </p>
      )}
      <ReportClaims report={report} />
    </>
  );
}

type InjectionFinding = {
  url: string;
  rationale: string;
};

/**
 * Reads the runner's injection findings. Written by the runner from the
 * judge's verdicts, never by the extraction model — which is the point, since
 * the model writing the report has just read the page doing the steering.
 */
function reportInjections(report: unknown): InjectionFinding[] {
  if (typeof report !== "object" || report === null || Array.isArray(report)) {
    return [];
  }
  const entries = (report as Record<string, unknown>)["injections"];
  if (!Array.isArray(entries)) return [];

  const out: InjectionFinding[] = [];
  for (const entry of entries) {
    if (typeof entry !== "object" || entry === null) continue;
    const record = entry as Record<string, unknown>;
    const url = record["url"];
    // Same rule as a citation: this is navigable, and it came from a page
    // already judged to be hostile.
    if (typeof url !== "string" || safeExternalHttpUrl(url) === null) continue;
    out.push({
      url,
      rationale:
        typeof record["rationale"] === "string" ? record["rationale"] : "",
    });
  }

  return out;
}

/**
 * Pages that tried to instruct the agent reading them. This is a finding
 * about the server rather than a note about the run: a vendor page that
 * attempts to steer a reviewer says more than any claim in the report, so it
 * sits above the summary rather than in the run metadata.
 */
function ReportInjections({ report }: { report: unknown }): JSX.Element | null {
  const injections = reportInjections(report);
  if (injections.length === 0) return null;

  return (
    <div className="border-warning mt-2 border px-2.5 py-1.5">
      <p className="font-medium">
        {injections.length === 1
          ? "A page tried to instruct the research agent."
          : `${injections.length} pages tried to instruct the research agent.`}{" "}
        <span className="text-muted-foreground font-normal">
          Content written to steer whoever reviews this server is itself
          evidence about it.
        </span>
      </p>
      <ul className="mt-1 space-y-1">
        {injections.map((injection) => (
          <li key={injection.url}>
            <ExternalCitationLink url={injection.url} />
            {injection.rationale && (
              <span className="text-muted-foreground">
                {" "}
                {injection.rationale}
              </span>
            )}
          </li>
        ))}
      </ul>
    </div>
  );
}

/** What produced the report, from its embedded run metadata. */
function ReportRunMeta({
  report,
}: {
  report: ResearchReport;
}): JSX.Element | null {
  if (report.status !== "completed") return null;

  const parts: string[] = [];
  if (report.model) parts.push(report.model);
  const raw = report.report;
  if (typeof raw === "object" && raw !== null && !Array.isArray(raw)) {
    const run = (raw as Record<string, unknown>)["run"];
    if (typeof run === "object" && run !== null && !Array.isArray(run)) {
      const meta = run as Record<string, unknown>;
      if (typeof meta["searches"] === "number") {
        parts.push(`${meta["searches"]} searches`);
      }
      if (typeof meta["fetches"] === "number") {
        parts.push(`${meta["fetches"]} pages read`);
      }
      const prompt = meta["prompt_tokens"];
      const completion = meta["completion_tokens"];
      if (typeof prompt === "number" && typeof completion === "number") {
        parts.push(`${(prompt + completion).toLocaleString()} tokens`);
      }
    }
  }
  if (parts.length === 0) return null;

  return (
    <p className="text-muted-foreground mt-2 text-xs">{parts.join(" · ")}</p>
  );
}

type ReportClaim = {
  text: string;
  tier?: string;
  citations: ClaimCitation[];
  sourceReputation?: string;
};

/**
 * Reads the version-1 report payload: a list of claims, each carrying a
 * provenance tier and its citations. Tolerant like the evidence parser — an
 * unrecognized shape renders nothing rather than crashing the page.
 */
function reportClaims(report: unknown): ReportClaim[] {
  if (typeof report !== "object" || report === null || Array.isArray(report)) {
    return [];
  }
  const claims = (report as Record<string, unknown>)["claims"];
  if (!Array.isArray(claims)) return [];

  const out: ReportClaim[] = [];
  for (const entry of claims) {
    if (typeof entry !== "object" || entry === null) continue;
    const record = entry as Record<string, unknown>;
    const text = record["text"];
    if (typeof text !== "string" || text === "") continue;
    out.push({
      text,
      tier: typeof record["tier"] === "string" ? record["tier"] : undefined,
      citations: citationURLs(record["citations"]),
      sourceReputation:
        typeof record["source_reputation"] === "string"
          ? record["source_reputation"]
          : undefined,
    });
  }

  return out;
}

/**
 * Reads a claim's citation list. The runner stores citations as {url, title}
 * objects; bare string URLs are accepted too so older payloads keep
 * rendering.
 */
function citationURLs(value: unknown): ClaimCitation[] {
  if (!Array.isArray(value)) return [];

  const out: ClaimCitation[] = [];
  for (const entry of value) {
    if (typeof entry === "string" && safeExternalHttpUrl(entry) !== null) {
      out.push({ url: entry });
      continue;
    }
    if (typeof entry === "object" && entry !== null && !Array.isArray(entry)) {
      const record = entry as Record<string, unknown>;
      const url = record["url"];
      if (typeof url === "string" && safeExternalHttpUrl(url) !== null) {
        out.push({
          url,
          trustedSource:
            typeof record["trusted_source"] === "string"
              ? record["trusted_source"]
              : undefined,
        });
      }
    }
  }

  return out;
}

type ClaimCitation = {
  url: string;
  /**
   * The category the citation's domain holds in the server's trusted-source
   * registry, stamped deterministically at validation — never by the model.
   * Absent means unlisted, not untrustworthy.
   */
  trustedSource?: string;
};

function tierLabel(tier: string): string {
  switch (tier) {
    case "independently_reported":
      return "Independently reported";
    case "vendor_claim":
      return "Vendor claim";
    case "community_report":
      return "Community report";
    default:
      return tier.replaceAll("_", " ");
  }
}

/**
 * The model's own judgment of the sources a claim rests on — never a curated
 * domain list. Unknown values, including absence on reports written before
 * the field existed, render nothing: unknown must never read as reputable.
 */
function reputationLabel(reputation: string): string | null {
  switch (reputation) {
    case "reputable":
      return "Reputable sources";
    case "mixed":
      return "Mixed-reputation sources";
    case "low":
      return "Low-reputation sources";
    default:
      return null;
  }
}

function SourceReputationLabel({
  reputation,
}: {
  reputation: string | undefined;
}): JSX.Element | null {
  const label = reputation === undefined ? null : reputationLabel(reputation);
  if (label === null) return null;

  return <span className="text-muted-foreground text-xs">{label}</span>;
}

function ReportClaims({ report }: { report: unknown }): JSX.Element | null {
  const claims = reportClaims(report);
  if (claims.length === 0) return null;

  return (
    <ul className="divide-border border-border mt-2 divide-y border-t">
      {claims.map((claim) => (
        <li key={claim.text} className="space-y-1 py-1.5">
          <p>{claim.text}</p>
          <div className="flex flex-wrap items-center gap-1.5">
            {claim.tier && (
              <span className="border-border text-muted-foreground border px-1.5 py-px text-xs">
                {tierLabel(claim.tier)}
              </span>
            )}
            {claim.citations.map((citation) => (
              <span
                key={citation.url}
                className="inline-flex items-center gap-1"
              >
                <ExternalCitationLink url={citation.url} truncate />
                {citation.trustedSource && (
                  <span className="text-muted-foreground text-xs">
                    ✓ {citation.trustedSource}
                  </span>
                )}
              </span>
            ))}
            <SourceReputationLabel reputation={claim.sourceReputation} />
          </div>
        </li>
      ))}
    </ul>
  );
}

/**
 * A link out to a page the research read. Opened through the shared safe path
 * rather than a plain anchor: these URLs come from material the runner treats
 * as hostile, so the tab is uniquely named, disowned, and navigated without a
 * referrer — and a popup the browser blocked is reported rather than looking
 * like a dead control.
 */
function ExternalCitationLink({
  url,
  truncate = false,
}: {
  url: string;
  truncate?: boolean;
}): JSX.Element {
  return (
    <button
      type="button"
      onClick={() => {
        if (!openSafeExternalUrl(url)) {
          toast.error("Your browser blocked opening this link in a new tab");
        }
      }}
      title={url}
      className={cn(
        "text-muted-foreground hover:text-foreground underline underline-offset-2",
        truncate && "truncate",
      )}
    >
      {citationHost(url)}
    </button>
  );
}

/** A citation renders as its host — the link itself carries the full URL. */
function citationHost(citation: string): string {
  try {
    return new URL(citation).host;
  } catch {
    return citation;
  }
}
