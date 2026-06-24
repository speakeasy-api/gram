// Shared presentational cells for risk/eval finding tables (AGE-2704).
//
// Both the realtime Risk Events table (RiskResult rows) and the policy-eval
// findings table (PolicyEvalFinding rows) render the same conceptual columns:
// category/rule, the matched span, confidence, tags, and a link to the chat
// session. These cells take plain props (not whole models) so a single
// implementation serves both surfaces, reusing the masking/category helpers in
// risk-ui / risk-utils.

import { Badge } from "@speakeasy-api/moonshine";
import { CategoryLabel, MaskedMatch } from "./risk-ui";

// Scanner sources whose matched span is sensitive (a secret or PII value) and
// must be masked behind a reveal toggle. Other sources (shadow_mcp, custom
// rules, the LLM judge) carry a non-sensitive excerpt that renders raw.
const SENSITIVE_SOURCES = new Set(["gitleaks", "presidio"]);

export function SourceRuleCell({
  source,
  ruleId,
}: {
  source?: string;
  ruleId?: string;
}): JSX.Element {
  return (
    <div className="flex min-w-0 items-center gap-2">
      <CategoryLabel source={source} ruleId={ruleId} />
      {ruleId && (
        <span className="text-muted-foreground truncate font-mono text-xs">
          {ruleId}
        </span>
      )}
    </div>
  );
}

// Renders the matched span. Sensitive sources go through MaskedMatch (needs a
// RevealAllProvider ancestor); everything else renders raw monospace text.
export function MatchCell({
  match,
  source,
}: {
  match?: string;
  source?: string;
}): JSX.Element {
  if (source && !SENSITIVE_SOURCES.has(source)) {
    return (
      <span
        className="text-muted-foreground block truncate font-mono text-xs"
        title={match}
      >
        {match ?? "—"}
      </span>
    );
  }
  return <MaskedMatch value={match} />;
}

export function ConfidenceCell({
  confidence,
}: {
  confidence?: number;
}): JSX.Element {
  return (
    <span className="text-sm">
      {confidence == null ? "—" : `${Math.round(confidence * 100)}%`}
    </span>
  );
}

export function TagsCell({ tags }: { tags?: string[] }): JSX.Element {
  if (!tags || tags.length === 0) {
    return <span className="text-muted-foreground text-sm">—</span>;
  }
  return (
    <div className="flex flex-wrap gap-1">
      {tags.map((tag) => (
        <Badge key={tag} variant="neutral">
          <Badge.Text>{tag}</Badge.Text>
        </Badge>
      ))}
    </div>
  );
}

// A clickable session cell that opens the chat detail sheet. When there's no
// chatId (an unlinkable finding) it falls back to plain text.
export function SessionCell({
  chatId,
  chatTitle,
  userId,
  onOpen,
}: {
  chatId?: string;
  chatTitle?: string;
  userId?: string;
  onOpen: (chatId: string) => void;
}): JSX.Element {
  const title =
    chatTitle ?? (chatId ? `${chatId.slice(0, 8)}…` : "Unknown session");
  const subtitle = userId ?? "unknown user";

  if (!chatId) {
    return (
      <div className="min-w-0">
        <div className="truncate text-sm">{title}</div>
        <div className="text-muted-foreground truncate text-xs">{subtitle}</div>
      </div>
    );
  }

  return (
    <button
      type="button"
      className="hover:text-foreground block min-w-0 text-left"
      onClick={(e) => {
        e.stopPropagation();
        onOpen(chatId);
      }}
    >
      <div className="truncate text-sm underline-offset-2 hover:underline">
        {title}
      </div>
      <div className="text-muted-foreground truncate text-xs">{subtitle}</div>
    </button>
  );
}
