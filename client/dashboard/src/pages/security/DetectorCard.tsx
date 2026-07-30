import { SimpleTooltip } from "@/components/ui/tooltip";
import { Switch } from "@/components/ui/switch";
import { cn } from "@/lib/utils";
import { Badge, Icon } from "@speakeasy-api/moonshine";
import type { IconName } from "@speakeasy-api/moonshine";
import {
  DETECTION_RULES,
  RULE_CATEGORY_META,
  type RuleCategory,
} from "./policy-data";
import { AVAILABLE_CATEGORIES } from "./policy-form";

export type DetectorCardProps = {
  category: RuleCategory;
  selected: boolean;
  disabledRules: Set<string>;
  disabledReason?: string;
  onToggle: (checked: boolean) => void;
  onCustomize: () => void;
};

export function DetectorCard({
  category,
  selected,
  disabledRules,
  disabledReason,
  onToggle,
  onCustomize,
}: DetectorCardProps): JSX.Element {
  const meta = RULE_CATEGORY_META[category];
  const available = AVAILABLE_CATEGORIES.has(category);
  const rules = DETECTION_RULES[category].filter((rule) => !rule.hidden);
  const customizable = available && rules.length > 1;
  const enabledCount = rules.filter(
    (rule) => !disabledRules.has(rule.id),
  ).length;
  const customized = selected && enabledCount < rules.length;
  const disabled = !available || disabledReason !== undefined;

  const toggle = (
    <Switch
      aria-label={`${meta.label} built-in rule`}
      checked={selected}
      disabled={disabled}
      onCheckedChange={onToggle}
    />
  );

  return (
    <div
      className={cn(
        "flex gap-3 rounded-lg border p-3 transition-colors",
        selected ? "border-foreground bg-muted/40" : "border-border",
      )}
    >
      <Icon
        name={meta.icon as IconName}
        className="text-muted-foreground mt-0.5 size-5 shrink-0"
      />
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className="text-sm font-medium">{meta.label}</span>
          {!available && (
            <Badge variant="neutral">
              <Badge.Text>Coming soon</Badge.Text>
            </Badge>
          )}
        </div>
        <p className="text-muted-foreground mt-0.5 text-xs">
          {meta.description}
        </p>
        <div className="mt-2 flex items-center gap-3 text-xs">
          {rules.length > 0 && (
            <span
              className={cn(
                "bg-muted rounded-full px-2 py-0.5",
                customized ? "text-foreground" : "text-muted-foreground",
              )}
            >
              {customized
                ? `${enabledCount} of ${rules.length} rules`
                : `${rules.length} rules`}
            </span>
          )}
          {selected && customizable && (
            <button
              type="button"
              onClick={onCustomize}
              className="text-primary hover:underline"
            >
              Customize
            </button>
          )}
        </div>
      </div>
      {disabledReason ? (
        <SimpleTooltip tooltip={disabledReason}>
          <span
            aria-label={`${meta.label} unavailable`}
            className="inline-flex cursor-not-allowed [&>button]:pointer-events-none"
            tabIndex={0}
          >
            {toggle}
          </span>
        </SimpleTooltip>
      ) : (
        toggle
      )}
    </div>
  );
}
