import { Eye, EyeOff } from "lucide-react";
import { useState } from "react";
import { RULE_CATEGORY_META } from "./policy-data";
import { getCategoryForFinding, getRuleTitleFallback } from "./risk-finding-utils";
import { humanizeRuleId } from "./rule-ids";

export function CategoryLabel({
  source,
  ruleId,
}: {
  source?: string;
  ruleId?: string;
}) {
  const category = getCategoryForFinding(source, ruleId);
  if (category) {
    return (
      <span className="font-mono text-xs">
        {RULE_CATEGORY_META[category].label}
      </span>
    );
  }
  // Unknown source: title-case it so the table cell still reads cleanly
  // (e.g. a future "presidio_pro" source renders as "Presidio Pro").
  return (
    <span className="font-mono text-xs">
      {source ? humanizeRuleId(source.replace(/_/g, "-")) : "-"}
    </span>
  );
}

// Renders a rule id with a tooltip-quality fallback when the dashboard
// hasn't seen this rule before. The backend may roll out new gitleaks,
// presidio, or prompt_injection rules independently of the dashboard, so
// every snake_case id needs to display legibly without a code change.
export function RuleLabel({ ruleId }: { source?: string; ruleId?: string }) {
  if (!ruleId) return <span className="font-mono text-xs">-</span>;
  const title = getRuleTitleFallback(ruleId);
  return (
    <span className="font-mono text-xs" title={ruleId}>
      {title}
    </span>
  );
}

export function MaskedMatch({ value }: { value: string | undefined }) {
  const [revealed, setRevealed] = useState(false);

  if (!value) return <span>-</span>;

  if (!revealed) {
    return (
      <button
        type="button"
        className="text-muted-foreground hover:text-foreground inline-flex items-center gap-1 text-xs"
        onClick={(e) => {
          e.stopPropagation();
          setRevealed(true);
        }}
      >
        <EyeOff className="h-3 w-3" />
        <span>Click to reveal</span>
      </button>
    );
  }

  return (
    <span className="inline-flex items-center gap-1">
      <span className="font-mono text-xs">
        {value.length > 40
          ? `${value.slice(0, 20)}...${value.slice(-10)}`
          : value}
      </span>
      <button
        type="button"
        className="text-muted-foreground hover:text-foreground"
        onClick={(e) => {
          e.stopPropagation();
          setRevealed(false);
        }}
      >
        <Eye className="h-3 w-3" />
      </button>
    </span>
  );
}
