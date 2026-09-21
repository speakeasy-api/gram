import { SimpleTooltip } from "@/components/ui/Tooltip";
import { Switch } from "@/components/ui/Switch";
import { cn } from "@/lib/utils";
import { Badge } from "@/components/ui/Badge";
import { Icon } from "@/components/ui/Icon";
import { type IconName } from "@/components/ui/Icon/names";
import {
  DETECTION_RULES,
  ruleCategoryMeta,
  type DetectionRule,
  type DetectorMode,
  type RuleCategory,
} from "./policy-data";
import { availableCategories, categoryLevelDetectors } from "./policy-form";

export type DetectorCardProps = {
  category: RuleCategory;
  selected: boolean;
  disabledRules: Set<string>;
  disabledReason?: string;
  /** Defaults to the legacy Presidio engine; see `DetectorMode`. */
  mode?: DetectorMode;
  onToggle: (checked: boolean) => void;
  onCustomize: () => void;
};

/** The rules the card can offer for customization. A category-level detector
 *  (under the LLM analyzer that includes `pii`) has no per-rule list. */
function customizableRules(
  category: RuleCategory,
  mode: DetectorMode,
): DetectionRule[] {
  if (categoryLevelDetectors(mode).has(category)) return [];
  return DETECTION_RULES[category].filter((rule) => !rule.hidden);
}

export function DetectorCard({
  category,
  selected,
  disabledRules,
  disabledReason,
  mode = "presidio",
  onToggle,
  onCustomize,
}: DetectorCardProps): JSX.Element {
  const meta = ruleCategoryMeta(category, mode);
  const available = availableCategories(mode).has(category);
  const rules = customizableRules(category, mode);
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
        "flex gap-3 border p-3 transition-colors",
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
