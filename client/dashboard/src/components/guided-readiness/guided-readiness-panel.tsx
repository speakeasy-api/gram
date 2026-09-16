import { Check, ChevronDown, ShieldAlert, X } from "lucide-react";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Skeleton } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { useIsPlatformAdmin } from "@/contexts/Auth";
import { cn } from "@/lib/utils";
import type { IdentityProviderReadiness } from "@gram/client/models/components/identityproviderreadiness.js";
import type {
  IdentityProviderReadinessCheck,
  Owner,
} from "@gram/client/models/components/identityproviderreadinesscheck.js";
import { useGuidedReadiness } from "./use-guided-readiness";

/** Who acts on a check that did not pass, in the words staff use for it. */
const OWNER_LABEL: Record<Owner, string> = {
  platform_admin: "Platform admin",
  customer: "Customer",
  speakeasy: "Speakeasy",
};

function formatTimestamp(value: Date): string {
  return value.toLocaleString([], {
    day: "numeric",
    month: "short",
    hour: "2-digit",
    minute: "2-digit",
  });
}

function CheckMark({ ok }: { ok: boolean }): JSX.Element {
  const Icon = ok ? Check : X;
  return (
    <div
      className={cn(
        "flex h-6 w-6 flex-shrink-0 items-center justify-center border",
        ok
          ? "border-success-default text-default-success"
          : "border-destructive-default text-default-destructive",
      )}
    >
      <Icon className="h-3.5 w-3.5" strokeWidth={3} aria-hidden="true" />
      <span className="sr-only">{ok ? "Passed" : "Did not pass"}</span>
    </div>
  );
}

function CheckRow({
  check,
}: {
  check: IdentityProviderReadinessCheck;
}): JSX.Element {
  return (
    <div className="border-border flex items-start gap-3 border-t py-3 first:border-t-0 first:pt-0">
      <CheckMark ok={check.ok} />
      <div className="min-w-0 flex-1 space-y-1">
        <div className="flex flex-wrap items-center gap-2">
          {/* The server's own key, not a name invented here: it is what a
              staff member quotes when they hand the pre-work on. */}
          <code className="text-foreground font-mono text-xs">{check.key}</code>
          <Badge variant="neutral" background size="sm">
            <Badge.Text>{OWNER_LABEL[check.owner]}</Badge.Text>
          </Badge>
        </div>
        <Text variant="small">{check.detail}</Text>
        {check.remedy ? (
          <Text variant="small" muted>
            {check.remedy}
          </Text>
        ) : null}
      </div>
    </div>
  );
}

function ReadinessBody({
  readiness,
}: {
  readiness: IdentityProviderReadiness;
}): JSX.Element {
  return (
    <div className="space-y-3">
      <Text variant="small" muted>
        {readiness.provider}
      </Text>
      <div className="border-border bg-card border px-4 py-1">
        {readiness.checks.map((check) => (
          <CheckRow key={check.key} check={check} />
        ))}
      </div>
    </div>
  );
}

function readErrorLine(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

/**
 * Why the guided Okta flow is or is not offered to this organization, for
 * Speakeasy staff only. It reports; it changes nothing. The customer sees the
 * outcome of these checks in the card itself and never the reasons.
 */
export function GuidedReadinessPanel(): JSX.Element | null {
  const isPlatformAdmin = useIsPlatformAdmin();
  // Gated here as well as in the wrapper so the read never fires for someone
  // who could not be shown the answer.
  const readiness = useGuidedReadiness(isPlatformAdmin);

  // The platform-admin flag is presentation only, so this gate is about who is
  // shown the answer; the endpoint behind it enforces the flag itself.
  if (!isPlatformAdmin) return null;

  let body: JSX.Element;
  if (readiness.isPending) {
    body = (
      <Skeleton>
        <div className="h-[180px] w-full" />
      </Skeleton>
    );
  } else if (readiness.error) {
    body = (
      <div className="space-y-1">
        <Text variant="small" destructive>
          Readiness could not be checked.
        </Text>
        <Text variant="small" muted>
          {readErrorLine(readiness.error)}
        </Text>
      </div>
    );
  } else if (readiness.data) {
    body = <ReadinessBody readiness={readiness.data} />;
  } else {
    body = (
      <Text variant="small" muted>
        Readiness has not been checked yet.
      </Text>
    );
  }

  let verdict: JSX.Element;
  if (readiness.data) {
    verdict = (
      <>
        <Badge
          variant={readiness.data.eligible ? "success" : "warning"}
          background
          size="sm"
        >
          <Badge.Text>
            {readiness.data.eligible ? "Eligible" : "Not eligible"}
          </Badge.Text>
        </Badge>
        {readiness.data.checkedAt ? (
          <Text variant="small" muted>
            checked {formatTimestamp(readiness.data.checkedAt)}
          </Text>
        ) : null}
      </>
    );
  } else {
    verdict = (
      <Text variant="small" muted>
        {readiness.isPending ? "checking..." : "not checked"}
      </Text>
    );
  }

  return (
    <details className="border-destructive-default bg-card group border">
      {/* One line closed. This sits above the provider grid on a page whose
          job is the grid, so it reports in a row and gets out of the way;
          opening it is what costs the reader vertical space. */}
      <summary className="flex cursor-pointer list-none flex-wrap items-center gap-2 px-3 py-2 [&::-webkit-details-marker]:hidden">
        <ShieldAlert
          className="text-default-destructive h-4 w-4 flex-shrink-0"
          aria-hidden="true"
        />
        <span className="sr-only">Platform admin only.</span>
        <Text variant="small" className="font-medium">
          Guided setup readiness
        </Text>
        {verdict}
        <ChevronDown
          className="text-muted-foreground ml-auto h-4 w-4 flex-shrink-0 transition-transform group-open:rotate-180"
          aria-hidden="true"
        />
      </summary>
      <div className="border-border space-y-3 border-t px-3 py-3">
        <div className="flex justify-end">
          <Button
            variant="tertiary"
            size="sm"
            onClick={() => readiness.refetch()}
            disabled={readiness.isFetching}
          >
            {readiness.isFetching ? "Re-checking..." : "Re-check"}
          </Button>
        </div>
        {body}
      </div>
    </details>
  );
}
