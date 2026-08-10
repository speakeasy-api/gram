import { DecisionForm } from "@/components/mcp-approvals/DecisionForm";
import {
  EvidencePanel,
  StatusBadge,
} from "@/components/mcp-approvals/EvidencePanel";
import { parseEvidenceDocument } from "@/components/mcp-approvals/evidence";
import { Button } from "@/components/ui/Button";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { useProject } from "@/contexts/Auth";
import { HumanizeDateTime } from "@/lib/dates";
import type { ApprovalDecision } from "@gram/client/models/components/approvaldecision.js";
import type { ApprovalRequester } from "@gram/client/models/components/approvalrequester.js";
import type { ResearchReport } from "@gram/client/models/components/researchreport.js";
import {
  invalidateGetMcpApprovalRequest,
  useGetMcpApprovalRequest,
} from "@gram/client/react-query/getMcpApprovalRequest.js";
import { invalidateAllListMcpApprovalRequests } from "@gram/client/react-query/listMcpApprovalRequests.js";
import { useRefreshMcpApprovalEvidenceMutation } from "@gram/client/react-query/refreshMcpApprovalEvidence.js";
import { useQueryClient } from "@tanstack/react-query";
import { ChevronDown, ChevronUp, RefreshCw } from "lucide-react";
import { useMemo, useState } from "react";
import { toast } from "sonner";

/**
 * One approval review: the evidence, everyone who asked, and every prior
 * decision. Rendered wherever a server is reviewed — the Shadow MCP server
 * page for URL targets, the queue's review sheet for stdio targets. Its job
 * is to make one decision fast and defensible, and never to let an absence
 * of evidence read as evidence of safety.
 *
 * showDecide renders the inline decision form. The server page leaves it off
 * — deciding there goes through the Decide Access sheet, and two decide
 * surfaces on one page would race each other — while the stdio sheet, which
 * has no other decide surface, turns it on.
 */
export function ApprovalReview({
  requestId,
  showDecide = false,
}: {
  requestId: string;
  showDecide?: boolean;
}): JSX.Element {
  const project = useProject();

  const detailQuery = useGetMcpApprovalRequest(
    { id: requestId, gramProject: project.slug },
    undefined,
    { enabled: requestId.length > 0 && project.slug.length > 0 },
  );

  const detail = detailQuery.data;
  const document = useMemo(
    () => parseEvidenceDocument(detail?.evidence, detail?.evidenceVersion),
    [detail],
  );

  if (!detail) {
    return <SkeletonTable />;
  }

  return (
    <div className="grid grid-cols-1 gap-6 lg:grid-cols-[2fr_1fr]">
      <div className="space-y-6">
        <EvidencePanel
          document={document}
          collectedAt={detail.evidenceCollectedAt}
        />
        <ResearchReports reports={detail.researchReports} />
      </div>
      <aside className="space-y-5">
        <RequestSummary
          status={detail.request.status}
          createdAt={detail.request.createdAt}
          versionPinned={detail.request.versionPinned}
        />
        <Requesters requesters={detail.requesters} />
        <PriorDecisions decisions={detail.decisions} />
        {showDecide && detail.request.status === "requested" && (
          <section className="space-y-2">
            <h3 className="text-eyebrow">Decide</h3>
            <DecisionForm
              requestId={detail.request.id}
              projectSlug={project.slug}
            />
          </section>
        )}
      </aside>
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

function RequestSummary({
  status,
  createdAt,
  versionPinned,
}: {
  status: string;
  createdAt: Date;
  versionPinned: boolean;
}): JSX.Element {
  return (
    <section className="space-y-2">
      <h3 className="text-eyebrow">Request</h3>
      <div className="border-border space-y-1.5 border p-3 text-xs">
        <div className="flex items-center justify-between">
          <span className="text-muted-foreground">Status</span>
          <StatusBadge status={status} />
        </div>
        <div className="flex items-center justify-between">
          <span className="text-muted-foreground">First raised</span>
          <HumanizeDateTime date={createdAt} includeTime={false} />
        </div>
        {!versionPinned && (
          <p className="text-muted-foreground border-border border-t pt-2 text-xs">
            No pinned version — what runs may differ from the evidence.
          </p>
        )}
      </div>
    </section>
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
        <ul className="space-y-3">
          {requesters.map((requester) => (
            <li
              key={requester.userId}
              className="border-border border p-2.5 text-xs"
            >
              <div className="flex items-center justify-between gap-2">
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
                <p className="text-muted-foreground mt-1">"{requester.note}"</p>
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
        <ul className="space-y-3">
          {decisions.map((decision) => (
            <li
              key={decision.id}
              className="border-border border p-2.5 text-xs"
            >
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
                <p className="text-muted-foreground mt-2">
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
}: {
  reports: ResearchReport[];
}): JSX.Element | null {
  if (reports.length === 0) {
    // No dead button until the research agent exists; the page is fully
    // usable on deterministic evidence alone.
    return null;
  }

  return (
    <section className="space-y-2">
      <h3 className="text-eyebrow">Research reports</h3>
      <p className="text-muted-foreground text-xs">
        Gathered by an agent from public web sources, which may be inaccurate,
        incomplete, or deliberately seeded. Read them as leads to verify, not as
        findings.
      </p>
      <ul className="space-y-3">
        {reports.map((report) => (
          <li key={report.id} className="border-border border p-2.5 text-xs">
            <div className="flex items-center justify-between gap-2">
              <span className="font-medium">
                {reportStatusLabel(report.status)}
              </span>
              <span className="text-muted-foreground shrink-0 text-xs">
                <HumanizeDateTime date={report.createdAt} />
              </span>
            </div>
            {report.error && (
              <p className="text-muted-foreground mt-1">{report.error}</p>
            )}
            <ReportClaims report={report.report} />
          </li>
        ))}
      </ul>
    </section>
  );
}

type ReportClaim = {
  text: string;
  tier?: string;
  citations: string[];
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
    const citations = record["citations"];
    out.push({
      text,
      tier: typeof record["tier"] === "string" ? record["tier"] : undefined,
      citations: Array.isArray(citations)
        ? citations.filter(
            (citation): citation is string => typeof citation === "string",
          )
        : [],
    });
  }

  return out;
}

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
              <a
                key={citation}
                href={citation}
                target="_blank"
                rel="noopener noreferrer"
                className="text-muted-foreground hover:text-foreground truncate underline underline-offset-2"
              >
                {citationHost(citation)}
              </a>
            ))}
          </div>
        </li>
      ))}
    </ul>
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
