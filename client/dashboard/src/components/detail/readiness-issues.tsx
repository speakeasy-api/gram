// oxlint-disable react/only-export-components -- context, provider and its markers belong together
import type { ReadinessCheck } from "@/components/mcp-server-readiness-bar";
import { cn } from "@/lib/utils";
import { Alert } from "@/components/ui/Alert";
import { createContext, use, type ReactNode } from "react";

/**
 * The header's open readiness checks, so the section a check links to is
 * marked in place. Without it, following "1 issue" to Settings lands on a
 * section that looks like every other one.
 */
const ReadinessIssuesContext = createContext<ReadinessCheck[]>([]);

export function ReadinessIssuesProvider({
  checks,
  pathname,
  children,
}: {
  checks: ReadinessCheck[];
  /**
   * The page being shown. Only checks that link to it count: a gateway's
   * check can link to a member server's page with an anchor this page has.
   */
  pathname: string;
  children: ReactNode;
}): JSX.Element {
  const here = checks.filter(
    (check) => (check.href ?? "").split("#")[0] === pathname,
  );
  return (
    <ReadinessIssuesContext value={here}>{children}</ReadinessIssuesContext>
  );
}

export type ReadinessIssueTone = "error" | "warning";

/** Open checks whose link lands on this anchor, and how loud to mark them. */
export function useReadinessIssuesFor(anchorId: string | undefined): {
  issues: ReadinessCheck[];
  tone: ReadinessIssueTone | undefined;
} {
  const checks = use(ReadinessIssuesContext);
  const issues = anchorId
    ? checks.filter(
        (check) =>
          check.ready === false &&
          !check.optional &&
          check.href?.split("#")[1] === anchorId,
      )
    : [];
  const tone =
    issues.length === 0
      ? undefined
      : issues.some((issue) => issue.broken)
        ? "error"
        : "warning";
  return { issues, tone };
}

/** The border a marked section's panel takes. */
export const READINESS_BORDER: Record<ReadinessIssueTone, string> = {
  error: "border-destructive",
  warning: "border-warning",
};

/**
 * The outline a marked field row takes. Drawn inside the row, so marking it
 * moves nothing and sits over the panel's own dividers.
 */
export const READINESS_OUTLINE: Record<ReadinessIssueTone, string> = {
  error: "outline outline-1 -outline-offset-1 outline-destructive",
  warning: "outline outline-1 -outline-offset-1 outline-warning",
};

/**
 * The header's issues for one anchor, as an error or warning sign with the
 * popover's words, for the line under a section's title.
 */
export function ReadinessIssueMarker({
  anchorId,
  className,
}: {
  anchorId: string | undefined;
  className?: string;
}): JSX.Element | null {
  const { issues, tone } = useReadinessIssuesFor(anchorId);
  if (issues.length === 0 || !tone) return null;
  // A callout, not a line of text: the section needs doing before the
  // server works, and a muted sentence above the panel read as a note.
  return (
    <div data-readiness-issue="" className={className}>
      <Alert
        variant={tone === "error" ? "error" : "warning"}
        title={
          issues.length === 1
            ? `Action needed: ${issues[0]!.label}`
            : "Action needed"
        }
      >
        {issues.length === 1 ? (
          here(issues[0]!.description)
        ) : (
          <ul className="list-disc space-y-1 pl-4">
            {issues.map((issue) => (
              <li key={issue.key}>
                <span className="font-medium">{issue.label}.</span>{" "}
                {here(issue.description)}
              </li>
            ))}
          </ul>
        )}
      </Alert>
    </div>
  );
}

/**
 * A check's words, read where they are shown: the marker sits on the very
 * page a check points to, so "in Settings" only says where the reader is.
 */
function here(description: string): string {
  return description.replace(/\s+(?:in|under) Settings(?=\.)/g, "");
}

/**
 * Marks an anchor that is not a `SettingsSection`: the sign and words on
 * top, and a coloured border around what it wraps.
 */
export function ReadinessIssueFrame({
  anchorId,
  children,
}: {
  anchorId: string;
  children: ReactNode;
}): JSX.Element {
  const { tone } = useReadinessIssuesFor(anchorId);
  // One element tree in every state: swapping it when the tone changes would
  // remount the children and drop what the user has typed. With no issue the
  // wrapper is `contents`, so it lays out as if it weren't there.
  return (
    <div
      className={
        tone ? cn("space-y-3 border p-4", READINESS_BORDER[tone]) : "contents"
      }
    >
      {tone ? <ReadinessIssueMarker anchorId={anchorId} /> : null}
      {children}
    </div>
  );
}
