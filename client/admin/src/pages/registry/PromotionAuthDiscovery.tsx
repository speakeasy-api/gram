import type { JSX } from "react";
import type { AdminRegistryPromotionCandidate } from "@gram/admin-client/models/components/adminregistrypromotioncandidate";
import { Button } from "@/components/ui/button";

export function PromotionAuthDiscovery({
  discovery,
  matchesSource,
  pending,
  disabled,
  onRetry,
}: {
  discovery: AdminRegistryPromotionCandidate["authDiscovery"];
  matchesSource: boolean;
  pending: boolean;
  disabled: boolean;
  onRetry: () => void;
}): JSX.Element {
  if (!matchesSource) {
    return (
      <section
        className="space-y-1 rounded-md border p-3 text-sm"
        aria-label="Sign-in setup"
      >
        <h3 className="font-medium">Sign-in setup</h3>
        <p className="text-muted-foreground">
          The endpoint or authentication choice differs from the source. Sign-in
          options will be checked for the reviewed endpoint when you promote it.
        </p>
      </section>
    );
  }
  const complete = discovery?.status === "complete";
  const noAuth = discovery?.authSetup === "none";
  const unknown = !discovery || discovery.authSetup === "unknown";
  const summary = promotionAuthSummary(discovery?.authSetup);
  return (
    <section
      className="space-y-2 rounded-md border p-3 text-sm"
      aria-label="Sign-in setup"
    >
      <h3 className="font-medium">Sign-in setup</h3>
      <p>{summary}</p>
      {!noAuth && (
        <dl className="grid grid-cols-2 gap-2">
          <div>
            <dt className="text-muted-foreground">
              Dynamic client registration (DCR)
            </dt>
            <dd>{capabilityLabel(discovery?.supportsDcr, complete)}</dd>
          </div>
          <div>
            <dt className="text-muted-foreground">Client ID metadata (CIMD)</dt>
            <dd>{capabilityLabel(discovery?.supportsCimd, complete)}</dd>
          </div>
        </dl>
      )}
      <p className="text-muted-foreground text-xs">
        These are discovered capabilities, not a completed sign-in test. No
        clients or credentials are created during review.
      </p>
      {unknown && (
        <Button
          type="button"
          variant="outline"
          disabled={pending || disabled}
          onClick={onRetry}
        >
          {pending ? "Checking sign-in options…" : "Recheck sign-in options"}
        </Button>
      )}
    </section>
  );
}

function capabilityLabel(
  supported: boolean | undefined,
  complete: boolean,
): string {
  if (supported) return "Available";
  return complete ? "Not available" : "Unknown";
}

function promotionAuthSummary(setup: string | undefined): string {
  switch (setup) {
    case "none":
      return "No authentication is configured for this public endpoint.";
    case "dcr":
      return "Automatic OAuth setup is available through dynamic client registration.";
    case "cimd":
      return "Automatic OAuth setup is available through client ID metadata.";
    case "manual":
      return "Manual OAuth client setup is required.";
    case undefined:
    default:
      return "Sign-in setup could not be confirmed. This does not mean automatic setup is unsupported.";
  }
}
