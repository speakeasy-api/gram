import { Link } from "react-router";
import { useProductFeatures } from "@gram/client/react-query/productFeatures.js";
import { LogDataRetentionBanner } from "@/components/observe/LoggingPageHeader";
import { Button } from "@/components/ui/Button";
import { useOrganization } from "@/contexts/Auth";
import { useOrgRoutes } from "@/routes";
import { EnableLoggingAndSessionCaptureSetting } from "./enable-logging-and-session-capture-setting";
import { StepSection } from "./step-section";

interface EnableLoggingSectionProps {
  index: number;
}

// Turning logging on is the precondition for every card that goes on to
// confirm traffic: with the bundle off, an instrumented agent still sends
// nothing to look at. Like publishing the marketplace, it is a prerequisite
// rather than an outcome of its own, so it opens each card that needs it
// instead of being a card admins have to find first.
export function EnableLoggingSection({
  index,
}: EnableLoggingSectionProps): JSX.Element {
  const organization = useOrganization();
  const orgRoutes = useOrgRoutes();
  const features = useProductFeatures(
    { organizationId: organization.id },
    undefined,
    { throwOnError: false },
  );
  const featuresLoading = features.isLoading;
  const featuresFailed =
    !featuresLoading && Boolean(features.error || !features.data);
  // Read straight from the query rather than remembering what the switch last
  // reported: it invalidates product features after every write, failed ones
  // included, so the server is always about to answer. An optimistic copy
  // would outlive a failed write, an admin disabling a feature elsewhere, or
  // an organization switch, and leave the step checked when it is not.
  const enabled =
    features.data?.logsEnabled === true &&
    features.data?.toolIoLogsEnabled === true &&
    features.data?.sessionCaptureEnabled === true;

  return (
    <StepSection
      index={index}
      slug="enable-logging"
      title="Enable logging"
      description="Record tool calls, I/O, and agent sessions. Nothing below can show traffic until this is on."
      complete={enabled}
    >
      <div className="space-y-4">
        <LogDataRetentionBanner />
        <div className="border-border bg-card border p-4">
          <EnableLoggingAndSessionCaptureSetting />
        </div>
        <p className="text-muted-foreground text-sm">
          This turns on Enable Logs, Record Tool I/O, and Agent Session Capture
          — the same settings as{" "}
          <Link
            to={orgRoutes.logs.href()}
            className="underline underline-offset-2"
          >
            Logging &amp; Telemetry
          </Link>
          . Fail Open, Hook Browser Sign-In, and other options stay on that
          page.
        </p>
        {featuresFailed ? (
          <div
            className="border-border border p-4"
            role="alert"
            aria-live="polite"
          >
            <p className="text-destructive text-sm">
              Couldn&apos;t load the current logging setting.
            </p>
            <Button
              variant="secondary"
              size="sm"
              className="mt-3"
              disabled={features.isFetching}
              onClick={() => void features.refetch()}
            >
              {features.isFetching ? "Retrying…" : "Retry"}
            </Button>
          </div>
        ) : null}
      </div>
    </StepSection>
  );
}
