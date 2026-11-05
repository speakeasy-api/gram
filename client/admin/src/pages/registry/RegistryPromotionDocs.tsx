import { useRef, type JSX } from "react";
import { useQueryClient } from "@tanstack/react-query";
import {
  CheckCircle2Icon,
  Clock3Icon,
  AlertCircleIcon,
  ExternalLinkIcon,
  RotateCwIcon,
} from "lucide-react";
import type { AdminRegistryPromotionCandidate } from "@gram/admin-client/models/components/adminregistrypromotioncandidate";
import { useRetryRegistryPromotionDocsMutation } from "@/lib/gramAdminClient";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { badgeTone } from "@/lib/badgeTone";
import {
  deliveryErrorMessage,
  deliveryStatus,
  promotionTimestamp,
} from "./registryPromotionLabels";

export function RegistryPromotionDocs({
  candidate,
  compact = false,
  onUpdated,
}: {
  candidate: AdminRegistryPromotionCandidate;
  compact?: boolean;
  onUpdated?: (candidate: AdminRegistryPromotionCandidate) => void;
}): JSX.Element {
  const queryClient = useQueryClient();
  const retry = useRetryRegistryPromotionDocsMutation();
  const inFlight = useRef(false);
  const requestKey = useRef({ revision: "", key: "" });
  const queued = candidate.docsStatus === "queued";
  const existing = candidate.docsStatus === "existing_guide";
  const inReview = candidate.docsStatus === "guide_in_review";
  const attention = candidate.docsStatus === "needs_attention";
  const stalledReview =
    attention && candidate.deliveryError === "docs_guide_pr_stalled";
  const resolved = queued || existing;
  const canRetry =
    Boolean(candidate.docsRevision) &&
    candidate.docsRetryAllowed &&
    (attention || candidate.docsStatus === "retrying");
  const Icon = resolved
    ? CheckCircle2Icon
    : attention
      ? AlertCircleIcon
      : Clock3Icon;
  const linkLabel = existing
    ? "View guide"
    : inReview || stalledReview
      ? "View guide PR"
      : queued
        ? "View guide request"
        : null;
  const next = promotionTimestamp(candidate.docsNextAttemptAt);
  const last = promotionTimestamp(candidate.docsLastAttemptAt);
  const queuedAt = promotionTimestamp(candidate.docsQueuedAt);
  const submit = async (): Promise<void> => {
    if (inFlight.current || !canRetry) return;
    inFlight.current = true;
    if (
      !requestKey.current.key ||
      requestKey.current.revision !== candidate.docsRevision
    )
      requestKey.current = {
        revision: candidate.docsRevision ?? "",
        key: crypto.randomUUID(),
      };
    try {
      const result = await retry.mutateAsync({
        request: {
          organizationId: candidate.organizationId,
          promotionId: candidate.id,
          docsRevision: candidate.docsRevision ?? "",
          retryKey: requestKey.current.key,
        },
      });
      onUpdated?.(result);
      await queryClient.invalidateQueries({
        predicate: (query) =>
          query.queryKey.some(
            (part) =>
              typeof part === "string" && /registry.*promotion/i.test(part),
          ),
      });
    } catch {
      /* Fixed error copy below; upstream errors may contain private details. */
    } finally {
      inFlight.current = false;
    }
  };
  const submitted =
    retry.isSuccess &&
    (retry.data?.docsRevision === candidate.docsRevision ||
      retry.variables?.request.docsRevision === candidate.docsRevision);
  return (
    <div
      className={
        compact
          ? "space-y-2 text-sm"
          : "space-y-3 rounded-lg border p-4 text-sm"
      }
    >
      <div className="flex items-center gap-2">
        <Icon aria-hidden="true" className="size-4 shrink-0" />
        <Badge
          variant="outline"
          className={
            resolved
              ? badgeTone.success
              : attention
                ? badgeTone.warning
                : badgeTone.neutral
          }
        >
          {deliveryStatus(candidate.docsStatus)}
        </Badge>
      </div>
      {!compact && (
        <p className="text-muted-foreground">
          {queued
            ? "The guide factory received this request. Follow the request for its draft and review; publication happens after the guide is merged."
            : existing
              ? "A setup guide for this slug already exists, so no new guide was requested. Guide refreshes go through the setup docs repository."
              : inReview
                ? "A guide pull request for this slug is already open, so no new guide was requested. Delivery checks it daily and records the guide once it merges."
                : attention
                  ? "The catalog entry is published. Resolve the delivery issue, then retry the guide request."
                  : "The catalog entry is published. Setup documentation will be requested automatically."}
        </p>
      )}
      {candidate.deliveryError && (
        <p className="text-muted-foreground">
          {deliveryErrorMessage(candidate.deliveryError)}
        </p>
      )}
      <div className="text-muted-foreground space-y-1 text-xs">
        {queuedAt ? (
          <p>
            {existing ? "Found" : "Requested"} {queuedAt}
          </p>
        ) : next ? (
          <p>
            {inReview ? "Next check" : "Next attempt"} {next}
          </p>
        ) : null}
        {last && !resolved && (
          <p>
            Last attempt {last} · {candidate.docsAttempts} attempt
            {candidate.docsAttempts === 1 ? "" : "s"}
          </p>
        )}
      </div>
      <div className="flex flex-wrap items-center gap-2">
        {linkLabel && candidate.docsUrl?.startsWith("https://") && (
          <Button variant="outline" size="sm" asChild>
            <a href={candidate.docsUrl} target="_blank" rel="noreferrer">
              {linkLabel}
              <ExternalLinkIcon aria-hidden="true" className="size-3" />
            </a>
          </Button>
        )}
        {canRetry && (
          <Button
            variant="outline"
            size="sm"
            disabled={retry.isPending || submitted}
            onClick={() => void submit()}
          >
            <RotateCwIcon aria-hidden="true" className="size-3" />
            {retry.isPending
              ? "Retrying…"
              : submitted
                ? "Retry scheduled"
                : "Retry guide request"}
          </Button>
        )}
      </div>
      {retry.isError && (
        <p role="alert">
          The guide request could not be retried. Refresh its status before
          trying again.
        </p>
      )}
      {submitted && (
        <p role="status">Guide request scheduled for another attempt.</p>
      )}
    </div>
  );
}
