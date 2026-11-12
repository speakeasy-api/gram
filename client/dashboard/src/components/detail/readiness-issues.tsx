// oxlint-disable react/only-export-components -- context, provider and its notice belong together
import { Alert } from "@/components/ui/Alert";
import type { ReadinessCheck } from "@/components/mcp-server-readiness-bar";
import { createContext, use, type ReactNode } from "react";

/**
 * The header's open readiness checks, so the section a check links to can
 * say the same thing in place. Without it, following "1 issue" to Settings
 * lands on a section that looks like every other one.
 */
const ReadinessIssuesContext = createContext<ReadinessCheck[]>([]);

export function ReadinessIssuesProvider({
  checks,
  children,
}: {
  checks: ReadinessCheck[];
  children: ReactNode;
}): JSX.Element {
  return (
    <ReadinessIssuesContext value={checks}>{children}</ReadinessIssuesContext>
  );
}

/** Open checks whose link lands on this anchor. */
function useReadinessIssuesFor(anchorId: string | undefined): ReadinessCheck[] {
  const checks = use(ReadinessIssuesContext);
  if (!anchorId) return [];
  return checks.filter(
    (check) =>
      check.ready === false &&
      !check.optional &&
      check.href?.split("#")[1] === anchorId,
  );
}

/**
 * The header's issues for one section, as the same warning or error the
 * popover shows. `SettingsSection` renders it under its title; anchors that
 * are not a `SettingsSection` render it themselves.
 */
export function ReadinessIssueNotice({
  anchorId,
}: {
  anchorId: string | undefined;
}): JSX.Element | null {
  const issues = useReadinessIssuesFor(anchorId);
  if (issues.length === 0) return null;
  return (
    <div className="space-y-2">
      {issues.map((issue) => (
        <Alert
          key={issue.key}
          variant={issue.broken ? "error" : "warning"}
          title={issue.label}
        >
          {issue.description}
        </Alert>
      ))}
    </div>
  );
}
