// Detector category card + per-rule customize sheet (AGE-2704).
// Moved verbatim from PolicyCenter.tsx.

import { Input } from "@/components/ui/input";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { Switch } from "@/components/ui/switch";
import { cn } from "@/lib/utils";
import { Badge, Icon } from "@speakeasy-api/moonshine";
import type { IconName } from "@speakeasy-api/moonshine";
import { ChevronRight } from "lucide-react";
import { useState } from "react";
import {
  DETECTION_RULES,
  RULE_CATEGORY_META,
  RULE_FAMILY_OF,
  RULE_FAMILY_ORDER,
  type DetectionRule,
  type RuleCategory,
} from "../policy-data";
import { AVAILABLE_CATEGORIES, HOOK_REQUIRED_CATEGORIES } from "./payload";

/** One built-in detector as a toggleable card (Detect step). "Customize" opens
 *  a side-sheet to pick which rules in the category are active. */
export function DetectorCard({
  category,
  selected,
  disabledRules,
  onToggle,
  onCustomize,
}: {
  category: RuleCategory;
  selected: boolean;
  disabledRules: Set<string>;
  onToggle: (checked: boolean) => void;
  onCustomize: () => void;
}): JSX.Element {
  const meta = RULE_CATEGORY_META[category];
  const available = AVAILABLE_CATEGORIES.has(category);
  const rules = DETECTION_RULES[category].filter((r) => !r.hidden);
  const customizable = available && rules.length > 1;
  const enabledCount = rules.filter((r) => !disabledRules.has(r.id)).length;
  const customized = selected && enabledCount < rules.length;
  const needsHook = HOOK_REQUIRED_CATEGORIES.has(category);
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
          {needsHook ? (
            <span className="text-warning">Requires Speakeasy hooks</span>
          ) : (
            rules.length > 0 && (
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
            )
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
      <Switch
        checked={selected}
        disabled={!available}
        onCheckedChange={onToggle}
      />
    </div>
  );
}

/** Side-sheet to pick which rules within a built-in detector category are
 *  active. Disabling a rule adds its canonical rule_id to the policy's
 *  disabled_rules; a search box tames the large categories (e.g. 222 secrets). */
export function CustomizeRulesSheet({
  category,
  selectedCategories,
  setSelectedCategories,
  disabledRules,
  setDisabledRules,
  onClose,
}: {
  category: RuleCategory;
  selectedCategories: Set<RuleCategory>;
  setSelectedCategories: (v: Set<RuleCategory>) => void;
  disabledRules: Set<string>;
  setDisabledRules: (v: Set<string>) => void;
  onClose: () => void;
}): JSX.Element {
  const meta = RULE_CATEGORY_META[category];
  const rules = DETECTION_RULES[category].filter((r) => !r.hidden);
  const [search, setSearch] = useState("");
  const query = search.trim().toLowerCase();
  const filtered = query
    ? rules.filter((r) => r.title.toLowerCase().includes(query))
    : rules;
  const enabledCount = rules.filter((r) => !disabledRules.has(r.id)).length;

  const setRule = (id: string, on: boolean) => {
    const next = new Set(disabledRules);
    if (on) {
      next.delete(id);
    } else {
      next.add(id);
    }
    setDisabledRules(next);
    if (on && !selectedCategories.has(category)) {
      const cats = new Set(selectedCategories);
      cats.add(category);
      setSelectedCategories(cats);
    }
  };
  const bulk = (on: boolean) => {
    const next = new Set(disabledRules);
    for (const r of rules) {
      if (on) {
        next.delete(r.id);
      } else {
        next.add(r.id);
      }
    }
    setDisabledRules(next);
  };

  // Large categories (currently just secrets, ~200 rules) classify into named
  // families so the list is navigable; everything else renders flat.
  const grouper = RULE_FAMILY_OF[category];
  const [expandedGroups, setExpandedGroups] = useState<Set<string>>(new Set());
  const toggleGroup = (family: string) => {
    const next = new Set(expandedGroups);
    if (next.has(family)) {
      next.delete(family);
    } else {
      next.add(family);
    }
    setExpandedGroups(next);
  };
  const bulkGroup = (familyRules: DetectionRule[], on: boolean) => {
    const next = new Set(disabledRules);
    for (const r of familyRules) {
      if (on) {
        next.delete(r.id);
      } else {
        next.add(r.id);
      }
    }
    setDisabledRules(next);
    if (on && !selectedCategories.has(category)) {
      const cats = new Set(selectedCategories);
      cats.add(category);
      setSelectedCategories(cats);
    }
  };
  // Ordered, non-empty families over the (search-)filtered rules.
  const groupedRules = grouper
    ? RULE_FAMILY_ORDER.map((family) => ({
        family,
        rules: filtered.filter((r) => grouper(r) === family),
      })).filter((g) => g.rules.length > 0)
    : [];

  return (
    <Sheet
      open
      onOpenChange={(o) => {
        if (!o) onClose();
      }}
    >
      <SheetContent side="right" className="flex flex-col p-0 sm:max-w-md">
        <SheetHeader className="px-6 pt-6">
          <SheetTitle>Customize {meta.label}</SheetTitle>
          <SheetDescription>
            Pick which rules in this category are active. All are on by default.
          </SheetDescription>
        </SheetHeader>
        <div className="px-6 pt-3">
          <Input
            value={search}
            onChange={setSearch}
            placeholder={`Search ${rules.length} ${meta.label.toLowerCase()} rules…`}
          />
        </div>
        <div className="text-muted-foreground flex items-center justify-between px-6 py-2 text-xs">
          <span>
            {enabledCount} of {rules.length} active
          </span>
          <span className="flex gap-3">
            <button
              type="button"
              className="text-primary hover:underline"
              onClick={() => bulk(true)}
            >
              Enable all
            </button>
            <button
              type="button"
              className="text-primary hover:underline"
              onClick={() => bulk(false)}
            >
              Disable all
            </button>
          </span>
        </div>
        <div className="flex-1 overflow-y-auto px-4 pb-6">
          {grouper
            ? groupedRules.map(({ family, rules: familyRules }) => {
                const open = expandedGroups.has(family) || query.length > 0;
                const enabled = familyRules.filter(
                  (r) => !disabledRules.has(r.id),
                ).length;
                return (
                  <div
                    key={family}
                    className="border-border border-b last:border-b-0"
                  >
                    <div className="flex items-center gap-2 px-2 py-2">
                      <button
                        type="button"
                        onClick={() => toggleGroup(family)}
                        className="flex min-w-0 flex-1 items-center gap-2 text-left"
                      >
                        <ChevronRight
                          className={cn(
                            "text-muted-foreground h-4 w-4 shrink-0 transition-transform",
                            open && "rotate-90",
                          )}
                        />
                        <span className="truncate text-sm font-medium">
                          {family}
                        </span>
                        <span className="text-muted-foreground shrink-0 text-xs">
                          {enabled}/{familyRules.length}
                        </span>
                      </button>
                      <Switch
                        checked={enabled === familyRules.length}
                        onCheckedChange={(on) => bulkGroup(familyRules, on)}
                      />
                    </div>
                    {open && (
                      <div className="pb-1 pl-4">
                        {familyRules.map((rule) => (
                          <RuleToggleRow
                            key={rule.id}
                            rule={rule}
                            checked={!disabledRules.has(rule.id)}
                            onToggle={(on) => setRule(rule.id, on)}
                          />
                        ))}
                      </div>
                    )}
                  </div>
                );
              })
            : filtered.map((rule) => (
                <RuleToggleRow
                  key={rule.id}
                  rule={rule}
                  checked={!disabledRules.has(rule.id)}
                  onToggle={(on) => setRule(rule.id, on)}
                />
              ))}
          {grouper && groupedRules.length === 0 && (
            <p className="text-muted-foreground px-2 py-6 text-center text-xs">
              No rules match.
            </p>
          )}
        </div>
      </SheetContent>
    </Sheet>
  );
}

export function RuleToggleRow({
  rule,
  checked,
  onToggle,
}: {
  rule: DetectionRule;
  checked: boolean;
  onToggle: (on: boolean) => void;
}): JSX.Element {
  return (
    <div className="hover:bg-muted flex items-center justify-between gap-3 rounded-md px-2 py-2 text-sm">
      <span className="min-w-0 truncate">{rule.title}</span>
      <Switch checked={checked} onCheckedChange={onToggle} />
    </div>
  );
}
