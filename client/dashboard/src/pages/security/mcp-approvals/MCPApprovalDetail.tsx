import { DecisionForm } from "@/components/mcp-approvals/DecisionForm";
import {
  EvidencePanel,
  StatusBadge,
} from "@/components/mcp-approvals/EvidencePanel";
import { parseEvidenceDocument } from "@/components/mcp-approvals/evidence";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { useProject } from "@/contexts/Auth";
import { HumanizeDateTime } from "@/lib/dates";
import type { ApprovalDecision } from "@gram/client/models/components/approvaldecision.js";
import type { ApprovalRequester } from "@gram/client/models/components/approvalrequester.js";
import type { ResearchReport } from "@gram/client/models/components/researchreport.js";
import { useGetMcpApprovalRequest } from "@gram/client/react-query/getMcpApprovalRequest.js";
import { useMemo } from "react";
import { useParams } from "react-router";

/**
 * One approval request: the evidence, everyone who asked, every prior
 * decision, and the decision form. The page's job is to make one decision
 * fast and defensible — and never to let an absence of evidence read as
 * evidence of safety.
 */
export default function MCPApprovalDetail(): JSX.Element {
  const { requestId = "" } = useParams<{ requestId: string }>();
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

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <RequireScope scope="mcp_approval:read" level="page">
          <Page.Section>
            <Page.Section.Title stage="preview">
              {detail?.request.targetRaw ?? "Approval request"}
            </Page.Section.Title>
            <Page.Section.Description>
              {detail
                ? `Requested by ${detail.request.requesterCount} ${detail.request.requesterCount === 1 ? "person" : "people"}.`
                : "Loading request."}
            </Page.Section.Description>
            <Page.Section.Body>
              {!detail ? (
                <SkeletonTable />
              ) : (
                <div className="grid grid-cols-1 gap-8 lg:grid-cols-[2fr_1fr]">
                  <div className="space-y-8">
                    <EvidencePanel
                      document={document}
                      collectedAt={detail.evidenceCollectedAt}
                    />
                    <ResearchReports reports={detail.researchReports} />
                  </div>
                  <aside className="space-y-8">
                    <RequestSummary
                      status={detail.request.status}
                      createdAt={detail.request.createdAt}
                      versionPinned={detail.request.versionPinned}
                    />
                    <Requesters requesters={detail.requesters} />
                    <PriorDecisions decisions={detail.decisions} />
                    {detail.request.status === "requested" && (
                      <section className="space-y-3">
                        <h3 className="text-eyebrow">Decide</h3>
                        <DecisionForm
                          requestId={detail.request.id}
                          projectSlug={project.slug}
                        />
                      </section>
                    )}
                  </aside>
                </div>
              )}
            </Page.Section.Body>
          </Page.Section>
        </RequireScope>
      </Page.Body>
    </Page>
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
    <section className="space-y-3">
      <h3 className="text-eyebrow">Request</h3>
      <div className="border-border space-y-2 border p-4 text-sm">
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
            This reference does not pin a version — what runs may differ from
            anything the evidence describes.
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
    <section className="space-y-3">
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
              className="border-border border p-3 text-sm"
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
    <section className="space-y-3">
      <h3 className="text-eyebrow">Have we decided on this before?</h3>
      {decisions.length === 0 ? (
        <p className="text-muted-foreground text-sm">
          No prior decisions. This is the first review of this server here.
        </p>
      ) : (
        <ul className="space-y-3">
          {decisions.map((decision) => (
            <li key={decision.id} className="border-border border p-3 text-sm">
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
              <p className="text-muted-foreground mt-1 text-xs">
                Decided on evidence as it stood then
                {decision.evidenceVersion !== undefined &&
                  ` (v${decision.evidenceVersion})`}
                ; a later re-gather does not rewrite it.
              </p>
            </li>
          ))}
        </ul>
      )}
    </section>
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
    <section className="space-y-3">
      <h3 className="text-eyebrow">Research reports</h3>
      <p className="text-muted-foreground text-xs">
        Gathered by an agent from public web sources, which may be inaccurate,
        incomplete, or deliberately seeded. Read them as leads to verify, not as
        findings.
      </p>
      <ul className="space-y-3">
        {reports.map((report) => (
          <li key={report.id} className="border-border border p-3 text-sm">
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
          </li>
        ))}
      </ul>
    </section>
  );
}
