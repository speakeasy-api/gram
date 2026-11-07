import { useRef, useState, type JSX, type ReactNode } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import type { AdminRegistryPromotionAccountUsage } from "@gram/admin-client/models/components/adminregistrypromotionaccountusage";
import {
  registryPromotionReviewQuery,
  useStartRegistryPromotionReviewMutation,
} from "@/lib/gramAdminClient";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { badgeTone } from "@/lib/badgeTone";
import { promotionTimestamp } from "./registryPromotionLabels";

type Target = {
  organizationId: string;
  projectId: string;
  mcpServerId: string;
};

const RESEARCH_POLL_MS = 15_000;

/** Aggregate usage from the account the server was found in. Staff only. */
export function PromotionAccountUsage({
  usage,
}: {
  usage: AdminRegistryPromotionAccountUsage | undefined;
}): JSX.Element {
  return (
    <section
      className="space-y-2 rounded-md border p-3 text-sm"
      aria-label="Source account usage"
    >
      <h3 className="font-medium">Source account usage</h3>
      {!usage ? (
        <p className="text-muted-foreground">
          Usage is unavailable. The account may have logging turned off.
        </p>
      ) : (
        <>
          <dl className="grid grid-cols-2 gap-2">
            <div>
              <dt className="text-muted-foreground">
                Users, last {usage.windowDays} days
              </dt>
              <dd>{usage.distinctUsers}</dd>
            </div>
            <div>
              <dt className="text-muted-foreground">Transport</dt>
              <dd>{usage.transport}</dd>
            </div>
            <div className="col-span-2">
              <dt className="text-muted-foreground">Clients</dt>
              <dd>{usage.clients.join(", ") || "None reported"}</dd>
            </div>
            {usage.resourceName && (
              <div className="col-span-2">
                <dt className="text-muted-foreground">Declared resource</dt>
                <dd>{usage.resourceName}</dd>
              </div>
            )}
          </dl>
          {usage.tools.length > 0 && (
            <div>
              <h4 className="text-muted-foreground">Most-called tools</h4>
              <ul>
                {usage.tools.map((tool) => (
                  <li key={tool.name} className="flex justify-between gap-2">
                    <span className="truncate font-mono text-xs">
                      {tool.name}
                    </span>
                    <span className="text-muted-foreground shrink-0">
                      {tool.successes} of {tool.calls} succeeded
                    </span>
                  </li>
                ))}
              </ul>
            </div>
          )}
          <p className="text-muted-foreground text-xs">
            For staff review only. Do not copy this into the public listing.
          </p>
        </>
      )}
    </section>
  );
}

/** The server's ordinary MCP review, held in Speakeasy's own organization. */
export function PromotionReview({
  target,
  allowed,
}: {
  target: Target;
  allowed: boolean;
}): JSX.Element {
  if (!allowed) {
    return (
      <ReviewSection>
        <p className="text-muted-foreground">
          This endpoint can&apos;t be reviewed because its URL may identify a
          customer.
        </p>
      </ReviewSection>
    );
  }
  return <AllowedPromotionReview target={target} />;
}

function AllowedPromotionReview({ target }: { target: Target }): JSX.Element {
  const queryClient = useQueryClient();
  const options = registryPromotionReviewQuery(target);
  const query = useQuery({
    ...options,
    refetchInterval: (state) =>
      state.state.data?.review?.research?.status === "running"
        ? RESEARCH_POLL_MS
        : false,
  });
  const mutation = useStartRegistryPromotionReviewMutation();
  const inFlight = useRef(false);
  const [failed, setFailed] = useState(false);
  const review = query.data?.review;
  const research = review?.research;
  const running = research?.status === "running";

  const start = async (): Promise<void> => {
    if (inFlight.current) return;
    inFlight.current = true;
    setFailed(false);
    try {
      const result = await mutation.mutateAsync({ request: target });
      queryClient.setQueryData(options.queryKey, result);
    } catch {
      setFailed(true);
    } finally {
      inFlight.current = false;
    }
  };

  return (
    <ReviewSection>
      <p className="text-muted-foreground">
        Gathers evidence and researches the vendor on the open web. It runs and
        is billed in speakeasy-team, and nothing appears in the customer&apos;s
        account.
      </p>
      {query.isError && (
        <p role="alert">Unable to load this review. Close and reopen it.</p>
      )}
      {research && <ResearchStatus status={research.status} />}
      {review?.evidence ? <ReviewEvidence evidence={review.evidence} /> : null}
      {research?.status === "failed" && research.error && (
        <p role="alert" className="text-xs">
          {research.error}
        </p>
      )}
      {research?.report ? <ResearchFindings report={research.report} /> : null}
      {failed && !running && (
        <p role="alert">The review could not be started. Try again.</p>
      )}
      <Button
        type="button"
        variant="outline"
        disabled={running || mutation.isPending || query.isPending}
        onClick={() => void start()}
      >
        {mutation.isPending
          ? "Gathering evidence…"
          : running
            ? "Research running…"
            : research
              ? "Run review again"
              : "Run review"}
      </Button>
    </ReviewSection>
  );
}

function ReviewSection({ children }: { children: ReactNode }): JSX.Element {
  return (
    <section
      className="space-y-2 rounded-md border p-3 text-sm"
      aria-label="MCP review"
    >
      <h3 className="font-medium">MCP review</h3>
      {children}
    </section>
  );
}

function ResearchStatus({ status }: { status: string }): JSX.Element {
  const tone =
    status === "completed"
      ? badgeTone.success
      : status === "failed"
        ? badgeTone.warning
        : badgeTone.neutral;
  const label =
    status === "completed"
      ? "Research complete"
      : status === "failed"
        ? "Research failed"
        : "Research running";
  return (
    <Badge variant="outline" className={tone}>
      {label}
    </Badge>
  );
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function text(value: unknown): string | undefined {
  return typeof value === "string" && value !== "" ? value : undefined;
}

function list(value: unknown): unknown[] {
  return Array.isArray(value) ? value : [];
}

/** Evidence is untrusted data, largely written by the reviewed server. */
function ReviewEvidence({
  evidence,
}: {
  evidence: unknown;
}): JSX.Element | null {
  if (!isRecord(evidence)) return null;
  const authority = isRecord(evidence.authority) ? evidence.authority : {};
  const provenance = isRecord(evidence.provenance) ? evidence.provenance : null;
  const domain = isRecord(evidence.domain) ? evidence.domain : {};
  const gaps = list(evidence.gaps).filter(
    (gap): gap is string => typeof gap === "string",
  );
  const registered = text(domain.registered_at);
  return (
    <dl className="grid grid-cols-2 gap-2">
      <div>
        <dt className="text-muted-foreground">Authentication</dt>
        <dd>{text(authority.mode) ?? "Unknown"}</dd>
      </div>
      <div>
        <dt className="text-muted-foreground">
          Tools declared without sign-in
        </dt>
        <dd>{list(evidence.capabilities).length}</dd>
      </div>
      <div>
        <dt className="text-muted-foreground">In a public registry</dt>
        <dd>
          {provenance ? (provenance.catalogued ? "Yes" : "No") : "Unknown"}
        </dd>
      </div>
      <div>
        <dt className="text-muted-foreground">Domain registered</dt>
        <dd>{registered ? promotionTimestamp(registered) : "Unknown"}</dd>
      </div>
      {gaps.length > 0 && (
        <div className="col-span-2">
          <dt className="text-muted-foreground">Could not check</dt>
          <dd>{gaps.join(", ")}</dd>
        </div>
      )}
    </dl>
  );
}

const CLAIM_TIERS: Record<string, string> = {
  independently_reported: "Independently reported",
  vendor_claim: "Vendor claim",
  observed: "Observed",
};

function safeCitationUrl(value: unknown): string | undefined {
  const raw = text(value);
  if (!raw) return undefined;
  try {
    return new URL(raw).protocol === "https:" ? raw : undefined;
  } catch {
    return undefined;
  }
}

/** Findings are cited web claims, never a verdict, and may be seeded. */
function ResearchFindings({ report }: { report: unknown }): JSX.Element | null {
  if (!isRecord(report)) return null;
  const summary = text(report.summary);
  const coverage = isRecord(report.coverage) ? report.coverage : {};
  const claims = list(report.claims).filter(isRecord);
  const injections = list(report.injections).filter(isRecord);
  if (!summary && claims.length === 0) return null;
  return (
    <div className="space-y-2">
      {summary && <p>{summary}</p>}
      {text(coverage.level) && (
        <p className="text-muted-foreground text-xs">
          Independent coverage: {text(coverage.level)}
        </p>
      )}
      {injections.length > 0 && (
        <p role="alert" className="text-xs">
          {injections.length} fetched page(s) tried to instruct the reviewer.
        </p>
      )}
      <ul className="space-y-2">
        {claims.map((claim, index) => (
          <li key={index}>
            <p className="text-muted-foreground text-xs">
              {CLAIM_TIERS[text(claim.tier) ?? ""] ?? "Unclassified"}
            </p>
            <p>{text(claim.text)}</p>
            <ul className="text-xs">
              {list(claim.citations)
                .filter(isRecord)
                .map((citation, citationIndex) => {
                  const url = safeCitationUrl(citation.url);
                  return url ? (
                    <li key={citationIndex}>
                      <a
                        className="underline"
                        href={url}
                        rel="noopener noreferrer nofollow"
                        target="_blank"
                      >
                        {text(citation.title) ?? url}
                      </a>
                    </li>
                  ) : null;
                })}
            </ul>
          </li>
        ))}
      </ul>
      <p className="text-muted-foreground text-xs">
        Web findings may be inaccurate or deliberately seeded. They inform your
        review and are not a verdict.
      </p>
    </div>
  );
}
