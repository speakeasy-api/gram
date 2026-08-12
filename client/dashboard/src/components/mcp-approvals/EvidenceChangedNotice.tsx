import { authorityModeLabel } from "@/components/mcp-approvals/evidence";
import { HumanizeDateTime } from "@/lib/dates";
import { cn } from "@/lib/utils";
import type { EvidenceDiff } from "@gram/client/models/components/evidencediff.js";
import type { EvidenceFieldChange } from "@gram/client/models/components/evidencefieldchange.js";
import { TriangleAlert } from "lucide-react";

const EVIDENCE_FIELD_LABELS: Record<string, string> = {
  authority_mode: "Authentication mode",
  dynamic_registration: "Dynamic client registration",
  known_advisories: "Published advisories",
};

function evidenceFieldLabel(change: EvidenceFieldChange): string {
  return EVIDENCE_FIELD_LABELS[change.field] ?? change.field;
}

/**
 * Renders one side of a scalar change. Authority modes get the same words
 * the evidence panel uses, and an empty mode — a server that publishes no
 * authority metadata at all, which is exactly the change worth seeing —
 * must read as a state rather than as a blank chip.
 */
function evidenceFieldValue(
  change: EvidenceFieldChange,
  value: string,
): string {
  if (change.field !== "authority_mode") {
    return value;
  }
  return authorityModeLabel(value);
}

/**
 * One term in the diff. The tone carries the meaning: what the server now
 * demands is highlighted, what it dropped is struck through, so the two are
 * distinguishable without reading the label above them.
 */
function DiffChip({
  tone,
  children,
}: {
  tone: "added" | "removed";
  children: React.ReactNode;
}): JSX.Element {
  return (
    <code
      className={cn(
        "border px-1 py-0.5 text-[11px]",
        tone === "added"
          ? "bg-warning/10 border-warning-default"
          : "border-border line-through opacity-70",
      )}
    >
      {children}
    </code>
  );
}

function DiffTermList({
  label,
  terms,
  tone,
}: {
  label: string;
  terms: string[];
  tone: "added" | "removed";
}): JSX.Element | null {
  if (terms.length === 0) {
    return null;
  }
  return (
    <div className="flex flex-wrap items-baseline gap-x-2 gap-y-1">
      <span className="text-muted-foreground">{label}</span>
      {terms.map((term) => (
        <DiffChip key={term} tone={tone}>
          {term}
        </DiffChip>
      ))}
    </div>
  );
}

/**
 * The re-review banner: the permission-relevant evidence no longer matches
 * what the standing approval rested on. Deliberately framed as a reason to
 * look again, never as a detection — a server whose published interface is
 * unchanged can still change its behavior, so the absence of this banner
 * guarantees nothing.
 */
export function EvidenceChangedNotice({
  diff,
  changedAt,
}: {
  diff: EvidenceDiff;
  changedAt?: Date | undefined;
}): JSX.Element {
  const fields = diff.fields ?? [];
  const advisories = diff.advisoriesAdded ?? [];

  return (
    <section
      role="alert"
      className="border-warning-default bg-warning/10 border-l-warning-default space-y-2.5 border border-l-4 p-4 text-xs"
    >
      <div className="flex items-center justify-between gap-2">
        <div className="text-default-warning flex items-center gap-2">
          <TriangleAlert className="size-4 shrink-0" />
          <p className="text-sm font-semibold tracking-wide uppercase">
            Changed since approval
          </p>
        </div>
        {changedAt && (
          <span className="text-muted-foreground shrink-0">
            first noticed{" "}
            <HumanizeDateTime date={changedAt} includeTime={false} />
          </span>
        )}
      </div>
      <p>
        This server's permission-relevant evidence no longer matches what the
        standing approval rested on. Look again and re-decide — a new decision
        accepts or revokes the change.
      </p>
      <div className="space-y-1.5">
        <DiffTermList
          label="Scopes added"
          terms={diff.scopesAdded ?? []}
          tone="added"
        />
        <DiffTermList
          label="Scopes removed"
          terms={diff.scopesRemoved ?? []}
          tone="removed"
        />
        <DiffTermList
          label="Credentials now demanded"
          terms={diff.secretsAdded ?? []}
          tone="added"
        />
        <DiffTermList
          label="Credentials no longer demanded"
          terms={diff.secretsRemoved ?? []}
          tone="removed"
        />
        {fields.map((change) => (
          <div
            key={change.field}
            className="flex flex-wrap items-baseline gap-x-2"
          >
            <span className="text-muted-foreground">
              {evidenceFieldLabel(change)}
            </span>
            <span className="flex items-baseline gap-1">
              <DiffChip tone="removed">
                {evidenceFieldValue(change, change.before)}
              </DiffChip>
              →
              <DiffChip tone="added">
                {evidenceFieldValue(change, change.after)}
              </DiffChip>
            </span>
          </div>
        ))}
        {advisories.length > 0 && (
          <div className="space-y-1">
            <span className="text-muted-foreground">New advisories</span>
            <ul className="space-y-1">
              {advisories.map((advisory) => (
                <li
                  key={advisory.id}
                  className="border-warning-default border px-2 py-1"
                >
                  <span className="font-medium">{advisory.id}</span>
                  {advisory.severity && (
                    <span className="text-muted-foreground">
                      {" "}
                      · {advisory.severity}
                    </span>
                  )}
                  {advisory.summary && (
                    <p className="text-muted-foreground mt-0.5">
                      {advisory.summary}
                    </p>
                  )}
                </li>
              ))}
            </ul>
          </div>
        )}
      </div>
    </section>
  );
}
