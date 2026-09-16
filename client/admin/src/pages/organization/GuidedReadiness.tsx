import type { JSX } from "react";
import { CheckIcon, XIcon } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import type { IdentityProviderReadiness } from "@gram/admin-client/models/components/identityproviderreadiness";
import type {
  IdentityProviderReadinessCheck,
  Owner,
} from "@gram/admin-client/models/components/identityproviderreadinesscheck";
import { useOrganizationGuidedReadiness } from "@/lib/guidedReadiness";
import { errorMessage } from "@/lib/gramAdminApi";
import { cn } from "@/lib/utils";

/** Who acts on a check that did not pass, in the words staff use for it. */
const OWNER_LABEL: Record<Owner, string> = {
  platform_admin: "Platform admin",
  customer: "Customer",
  speakeasy: "Speakeasy",
};

function fmtCheckedAt(value: Date): string {
  return value.toLocaleString(undefined, {
    timeZone: "UTC",
    day: "numeric",
    month: "short",
    hour: "2-digit",
    minute: "2-digit",
  });
}

function CheckMark({ ok }: { ok: boolean }): JSX.Element {
  const Icon = ok ? CheckIcon : XIcon;
  return (
    <span
      className={cn(
        "mt-0.5 flex size-5 shrink-0 items-center justify-center",
        ok ? "text-emerald-700 dark:text-emerald-400" : "text-destructive",
      )}
    >
      <Icon className="size-4" aria-hidden="true" />
      <span className="sr-only">{ok ? "Passed" : "Did not pass"}</span>
    </span>
  );
}

function CheckRow({
  check,
}: {
  check: IdentityProviderReadinessCheck;
}): JSX.Element {
  return (
    <div className="flex items-start gap-3 border-t py-3 first:border-t-0 first:pt-0">
      <CheckMark ok={check.ok} />
      <div className="min-w-0 flex-1 space-y-1">
        <div className="flex flex-wrap items-center gap-2">
          {/* The server's own key, not a name invented here: it is what an
              operator quotes when they hand the pre-work on. */}
          <code className="font-mono text-xs">{check.key}</code>
          <Badge variant="outline">{OWNER_LABEL[check.owner]}</Badge>
        </div>
        <p className="text-sm">{check.detail}</p>
        {check.remedy && (
          <p className="text-muted-foreground text-sm">{check.remedy}</p>
        )}
      </div>
    </div>
  );
}

function ReadinessFacts({
  readiness,
}: {
  readiness: IdentityProviderReadiness;
}): JSX.Element {
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <Badge variant={readiness.eligible ? "default" : "destructive"}>
          {readiness.eligible ? "Eligible" : "Not eligible"}
        </Badge>
        <span className="text-muted-foreground text-sm">
          {readiness.provider} · checked {fmtCheckedAt(readiness.checkedAt)}
        </span>
      </div>
      <div>
        {readiness.checks.map((check) => (
          <CheckRow key={check.key} check={check} />
        ))}
      </div>
    </div>
  );
}

/**
 * Why the guided Okta flow is or is not offered to this organization. The same
 * answer the customer's own dashboard shows Speakeasy staff, for an operator
 * who is looking at the record rather than the tenant. It reports; it changes
 * nothing.
 */
export function GuidedReadinessFacts({
  organizationID,
}: {
  organizationID: string;
}): JSX.Element {
  const readiness = useOrganizationGuidedReadiness(organizationID);

  let body: JSX.Element;
  if (readiness.isPending) {
    body = <Skeleton className="h-36 w-full" />;
  } else if (readiness.error) {
    body = (
      <div className="space-y-1">
        <p className="text-destructive text-sm">
          Readiness could not be checked.
        </p>
        <p className="text-muted-foreground text-sm">
          {errorMessage(readiness.error)}
        </p>
      </div>
    );
  } else if (readiness.data) {
    body = <ReadinessFacts readiness={readiness.data} />;
  } else {
    body = (
      <p className="text-muted-foreground text-sm">
        Readiness has not been checked yet.
      </p>
    );
  }

  return (
    <div className="space-y-4">
      {body}
      <Button
        variant="outline"
        size="sm"
        disabled={readiness.isFetching}
        onClick={() => readiness.refetch()}
      >
        {readiness.isFetching ? "Re-checking…" : "Re-check"}
      </Button>
    </div>
  );
}
