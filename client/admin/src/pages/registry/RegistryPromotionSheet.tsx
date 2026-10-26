import { useRef, useState, type JSX } from "react";
import { useBlocker } from "@tanstack/react-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import type { AdminRegistryPromotionCandidate } from "@gram/admin-client/models/components/adminregistrypromotioncandidate";
import {
  registryPromotionCandidateQuery,
  usePromoteRegistryCandidateMutation,
} from "@/lib/gramAdminClient";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetDescription,
} from "@/components/ui/sheet";
import { promotionReason, promotionTimestamp } from "./registryPromotionLabels";
import { RegistryPromotionDocs } from "./RegistryPromotionDocs";
import { Badge } from "@/components/ui/badge";
import { badgeTone } from "@/lib/badgeTone";

export type PromotionTarget = {
  organizationId: string;
  projectId: string;
  mcpServerId: string;
};

export function RegistryPromotionSheet({
  target,
  onClose,
}: {
  target: PromotionTarget;
  onClose: () => void;
}): JSX.Element {
  const queryClient = useQueryClient();
  const query = useQuery(registryPromotionCandidateQuery(target));
  const mutation = usePromoteRegistryCandidateMutation();
  const [result, setResult] = useState<AdminRegistryPromotionCandidate | null>(
    null,
  );
  const [form, setForm] = useState({
    publicName: "",
    publicTitle: "",
    publicDescription: "",
    publicUrl: "",
    documentationUrl: "",
    docsSlug: "",
  });
  const [authMode, setAuthMode] = useState<"" | "none" | "oauth">("");
  const [initialMetadata, setInitialMetadata] = useState<{
    form: typeof form;
    authMode: "" | "none" | "oauth";
  } | null>(null);
  const candidate = result ?? query.data;
  if (query.data && initialMetadata === null) {
    const suggested = query.data.suggestedMetadata;
    const initialForm = {
      publicName: suggested?.publicName ?? "",
      publicTitle: suggested?.publicTitle ?? "",
      publicDescription: suggested?.publicDescription ?? "",
      publicUrl: suggested?.publicUrl ?? "",
      documentationUrl: suggested?.documentationUrl ?? "",
      docsSlug: suggested?.docsSlug ?? "",
    };
    const initialAuth =
      suggested?.authMode === "oauth" || suggested?.authMode === "none"
        ? suggested.authMode
        : "";
    setInitialMetadata({ form: initialForm, authMode: initialAuth });
    setForm(initialForm);
    setAuthMode(initialAuth);
  }
  const [confirmed, setConfirmed] = useState(false);
  const [overrideEligibility, setOverrideEligibility] = useState(false);
  const canOverride =
    candidate?.status === "candidate" &&
    !candidate.eligible &&
    candidate.canOverrideEligibility;
  const canPromote =
    candidate?.eligible || (canOverride && overrideEligibility);
  const [reviewedRevision, setReviewedRevision] = useState("");
  const [reviewedCurationRevision, setReviewedCurationRevision] = useState("");
  const [failure, setFailure] = useState(false);
  const [refreshRequired, setRefreshRequired] = useState(false);
  const inFlight = useRef(false);
  const retryKey = useRef(crypto.randomUUID());
  const dirty =
    !result &&
    initialMetadata !== null &&
    (authMode !== initialMetadata.authMode ||
      (Object.keys(form) as (keyof typeof form)[]).some(
        (field) => form[field] !== initialMetadata.form[field],
      ));
  useBlocker({
    shouldBlockFn: () =>
      inFlight.current ||
      (dirty && !window.confirm("Discard this promotion review?")),
    enableBeforeUnload: dirty || mutation.isPending,
  });
  const close = (): void => {
    if (
      !inFlight.current &&
      (!dirty || window.confirm("Discard this promotion review?"))
    )
      onClose();
  };
  const change = (field: keyof typeof form, value: string): void => {
    setForm((previous) => ({ ...previous, [field]: value }));
    setConfirmed(false);
    retryKey.current = crypto.randomUUID();
  };
  const submit = async (): Promise<void> => {
    if (
      !candidate ||
      !canPromote ||
      candidate.status !== "candidate" ||
      !authMode ||
      !confirmed ||
      inFlight.current ||
      refreshRequired
    )
      return;
    inFlight.current = true;
    setFailure(false);
    try {
      const promoted = await mutation.mutateAsync({
        request: {
          ...target,
          ...form,
          authMode,
          sourceRevision: reviewedRevision,
          curationRevision: reviewedCurationRevision,
          publicMetadataConfirmed: true,
          overrideEligibility: canOverride && overrideEligibility,
          retryKey: retryKey.current,
        },
      });
      setResult(promoted);
      await queryClient.invalidateQueries({
        predicate: (entry) =>
          entry.queryKey.some(
            (part) =>
              typeof part === "string" &&
              (part.includes("Registry") || part.includes("registry")),
          ),
      });
    } catch (error) {
      setFailure(true);
      if (
        error &&
        typeof error === "object" &&
        "statusCode" in error &&
        error.statusCode === 409
      )
        setRefreshRequired(true);
    } finally {
      inFlight.current = false;
    }
  };
  const refreshDelivery = async (): Promise<void> => {
    const fresh = await query.refetch();
    if (fresh.data) setResult(fresh.data);
  };
  const refreshEligibility = async (): Promise<void> => {
    const fresh = await query.refetch();
    if (!fresh.error) {
      setRefreshRequired(false);
      setOverrideEligibility(false);
      setConfirmed(false);
      retryKey.current = crypto.randomUUID();
    }
  };
  const fields = [
    ["publicName", "Public catalog name", "com.example/server"],
    ["publicTitle", "Public title", "Example MCP server"],
    ["publicUrl", "Public MCP endpoint", "https://mcp.example.com/mcp"],
    [
      "documentationUrl",
      "Official documentation URL",
      "https://docs.example.com/mcp",
    ],
    ["docsSlug", "Setup documentation slug", "example-mcp"],
  ] as const;
  return (
    <Sheet
      open
      onOpenChange={(open) => {
        if (!open) close();
      }}
    >
      <SheetContent className="w-full overflow-y-auto sm:max-w-xl">
        <SheetHeader>
          <SheetTitle>
            {candidate?.status === "promoted"
              ? "Publication details"
              : "Review catalog promotion"}
          </SheetTitle>
          <SheetDescription>
            Review a public listing for this remote MCP server.
          </SheetDescription>
        </SheetHeader>
        <div className="space-y-4 px-4 pb-4">
          <p className="text-muted-foreground text-sm">
            The catalog and setup documentation are public. Enter
            vendor-approved details without customer identifiers, credentials,
            or tenant-specific URLs. Only public endpoints with no
            authentication or reusable OAuth are supported.
          </p>
          {query.isPending && (
            <p role="status">Checking promotion eligibility…</p>
          )}
          {query.isError && (
            <div role="alert">
              <p>Unable to check promotion eligibility.</p>
              <Button variant="outline" onClick={() => void query.refetch()}>
                Retry eligibility check
              </Button>
            </div>
          )}
          {candidate && candidate.status !== "promoted" && (
            <div className="rounded-md border p-3 text-sm">
              {candidate.sourceName && (
                <p className="mb-2 font-medium">{candidate.sourceName}</p>
              )}
              <Badge
                variant="outline"
                className={
                  candidate.eligible ? badgeTone.success : badgeTone.warning
                }
              >
                {promotionReason(candidate.reason)}
              </Badge>
              <p className="text-muted-foreground">
                Requires 10 successful calls across 2 UTC days, no failures, and
                activity within 48 hours. Evidence covers up to 14 days since
                the current configuration.
              </p>
              <p>
                {candidate.observedSuccesses} successful calls ·{" "}
                {candidate.observedFailures} failed calls ·{" "}
                {candidate.observedDays} active days
              </p>
              {candidate.checkedAt && (
                <p className="text-muted-foreground text-xs">
                  Checked {promotionTimestamp(candidate.checkedAt)}
                </p>
              )}
            </div>
          )}
          {(candidate?.status === "rejected" ||
            candidate?.status === "archived") && (
            <p role="status" className="rounded-md border p-3 text-sm">
              This candidate is {candidate.status}. Restore it from the
              promotion queue before reviewing it for publication.
            </p>
          )}
          {canOverride && (
            <div className="space-y-2 rounded-md border border-amber-500/40 p-3 text-sm">
              <p>
                This server does not meet the health criteria. You can promote
                it anyway after reviewing the evidence and verifying the public
                listing. This override is recorded in the audit log.
              </p>
              <label className="flex items-start gap-2">
                <Checkbox
                  checked={overrideEligibility}
                  disabled={mutation.isPending || query.isFetching}
                  onCheckedChange={(value) => {
                    setOverrideEligibility(value === true);
                    setConfirmed(false);
                    retryKey.current = crypto.randomUUID();
                  }}
                />
                <span>Promote anyway despite the health criteria</span>
              </label>
            </div>
          )}
          {candidate?.status === "promoted" ? (
            <div className="space-y-2">
              <div className="space-y-1">
                <Badge variant="outline" className={badgeTone.success}>
                  Catalog entry published
                </Badge>
                {candidate.publicTitle && (
                  <p className="text-lg font-medium">{candidate.publicTitle}</p>
                )}
                {candidate.promotedAt && (
                  <p className="text-muted-foreground text-xs">
                    Published {promotionTimestamp(candidate.promotedAt)}
                  </p>
                )}
              </div>
              <RegistryPromotionDocs
                candidate={candidate}
                onUpdated={setResult}
              />
              <Button
                variant="outline"
                disabled={query.isFetching}
                onClick={() => void refreshDelivery()}
              >
                Refresh delivery status
              </Button>
            </div>
          ) : candidate?.status === "candidate" && canPromote ? (
            <form
              className="space-y-3"
              onSubmit={(event) => {
                event.preventDefault();
                void submit();
              }}
            >
              <div>
                <h3 className="font-medium">Public listing</h3>
                <p className="text-muted-foreground text-sm">
                  Suggestions are untrusted draft values from the source server.
                  Review and edit every field before publishing. Complete the
                  description and any missing documentation URL. These details
                  appear in the public catalog and guide request.
                </p>
              </div>
              {fields.map(([field, label, placeholder]) => (
                <label key={field} className="block space-y-1 text-sm">
                  <span>{label}</span>
                  <Input
                    required
                    value={form[field]}
                    placeholder={placeholder}
                    disabled={mutation.isPending}
                    type={field.endsWith("Url") ? "url" : "text"}
                    maxLength={
                      field === "publicName" || field === "publicTitle"
                        ? 200
                        : field === "docsSlug"
                          ? 80
                          : 2048
                    }
                    onChange={(event) => change(field, event.target.value)}
                  />
                </label>
              ))}
              <label className="block space-y-1 text-sm">
                <span>Public description</span>
                <Textarea
                  required
                  maxLength={2000}
                  value={form.publicDescription}
                  disabled={mutation.isPending}
                  onChange={(event) =>
                    change("publicDescription", event.target.value)
                  }
                />
              </label>
              <div className="space-y-1 text-sm">
                <span id="promotion-auth-label">Public authentication</span>
                <Select
                  value={authMode}
                  disabled={mutation.isPending}
                  onValueChange={(value) => {
                    if (value === "none" || value === "oauth") {
                      setAuthMode(value);
                      setConfirmed(false);
                      retryKey.current = crypto.randomUUID();
                    }
                  }}
                >
                  <SelectTrigger aria-labelledby="promotion-auth-label">
                    <SelectValue placeholder="Select authentication" />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="none">No authentication</SelectItem>
                    <SelectItem value="oauth">OAuth</SelectItem>
                  </SelectContent>
                </Select>
              </div>
              <label className="flex items-start gap-2 text-sm">
                <Checkbox
                  checked={confirmed}
                  disabled={mutation.isPending}
                  onCheckedChange={(value) => {
                    setConfirmed(value === true);
                    setReviewedRevision(candidate.sourceRevision);
                    setReviewedCurationRevision(candidate.curationRevision);
                  }}
                />
                <span>
                  I verified that these details may be published publicly and
                  contain no customer data or secrets.
                </span>
              </label>
              {failure && (
                <p role="alert">
                  Promotion could not be completed. Your review has been
                  retained.
                  {refreshRequired
                    ? " The source changed. Refresh eligibility before trying again."
                    : " Check the details or retry."}
                </p>
              )}
              {refreshRequired && (
                <Button
                  type="button"
                  variant="outline"
                  disabled={query.isFetching}
                  onClick={() => void refreshEligibility()}
                >
                  Refresh eligibility
                </Button>
              )}
              <Button
                type="submit"
                disabled={
                  !confirmed ||
                  !authMode ||
                  mutation.isPending ||
                  refreshRequired ||
                  query.isFetching
                }
              >
                {mutation.isPending
                  ? "Promoting…"
                  : canOverride
                    ? "Promote anyway"
                    : "Publish catalog entry"}
              </Button>
            </form>
          ) : null}
          <Button
            variant="outline"
            disabled={mutation.isPending}
            onClick={close}
          >
            {result ? "Done" : "Close"}
          </Button>
        </div>
      </SheetContent>
    </Sheet>
  );
}

export function RegistryPromotionButton({
  target,
}: {
  target: PromotionTarget;
}): JSX.Element {
  const [open, setOpen] = useState(false);
  return (
    <>
      <Button variant="outline" onClick={() => setOpen(true)}>
        Review catalog promotion
      </Button>
      {open && (
        <RegistryPromotionSheet
          key={`${target.organizationId}/${target.projectId}/${target.mcpServerId}`}
          target={target}
          onClose={() => setOpen(false)}
        />
      )}
    </>
  );
}
