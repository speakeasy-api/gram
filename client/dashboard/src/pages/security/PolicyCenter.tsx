import * as DialogPrimitive from "@radix-ui/react-dialog";
import { InsightsConfig } from "@/components/insights-dock";
import { INSIGHTS_SUGGESTIONS } from "@/lib/insights-suggestions";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { SearchBar } from "@/components/ui/search-bar";
import { Slider } from "@/components/ui/slider";
import { Switch } from "@/components/ui/switch";
import { TextArea } from "@/components/ui/textarea";
import { SimpleTooltip } from "@/components/ui/tooltip";
import {
  OnboardingStepper,
  type Step,
} from "@/pages/setup/components/onboarding-stepper";
import { useInsightsState } from "@/components/insights-context";
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetDescription,
} from "@/components/ui/sheet";
import { Type } from "@/components/ui/type";
import {
  PageTabsTrigger,
  Tabs,
  TabsContent,
  TabsList,
} from "@/components/ui/tabs";
import { ExclusionsTab, type ExclusionSheetState } from "./ExclusionsTab";
import {
  Badge,
  Button,
  type Column,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
  Icon,
  Table,
} from "@speakeasy-api/moonshine";
import type { BadgeProps, IconName } from "@speakeasy-api/moonshine";
import {
  ArrowLeft,
  Plus,
  Shield,
  Ellipsis,
  Info,
  Loader2,
  ChevronRight,
  RefreshCw,
  Sparkles,
  SlidersHorizontal,
  X,
} from "lucide-react";
import {
  useState,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useLayoutEffect,
  type ReactNode,
} from "react";
import { parseAsString, parseAsStringLiteral, useQueryStates } from "nuqs";
import { useNavigate } from "react-router";
import { useQueryClient } from "@tanstack/react-query";
import {
  invalidateAllRiskListPolicies,
  useMembers,
  useRiskCreatePolicyMutation,
  useRiskListPolicies,
  useRiskPoliciesDeleteMutation,
  useRiskPoliciesUpdateMutation,
  useRoles,
} from "@gram/client/react-query/index.js";
import {
  useRiskPoliciesStatus,
  invalidateAllRiskPoliciesStatus,
} from "@gram/client/react-query/riskPoliciesStatus.js";
import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import type { Role } from "@gram/client/models/components/role.js";
import {
  RULE_CATEGORY_META,
  DETECTION_RULES,
  POLICY_MESSAGE_TYPE_META,
  RULE_FAMILY_OF,
  RULE_FAMILY_ORDER,
  type DetectionRule,
  type RuleCategory,
  type PolicyAction,
  type PolicyMessageType,
} from "./policy-data";
import { cn } from "@/lib/utils";
import { ruleIdToPresidioEntity } from "./rule-ids";
import { useDetectionRulesStore } from "./detection-rules-data";
import { CelExpressionField } from "./cel-field";
import { useCelStatus } from "./use-cel-status";
import { useTelemetry } from "@/contexts/Telemetry";
import { PROMPT_POLICY_TEMPLATES } from "./prompt-policy-templates";

/** Presidio-backed categories */
const PRESIDIO_CATEGORIES: RuleCategory[] = [
  "financial",
  "pii",
  "government_ids",
  "healthcare",
];

/** Categories that are currently available */
const AVAILABLE_CATEGORIES: Set<RuleCategory> = new Set([
  "secrets",
  ...PRESIDIO_CATEGORIES,
  "shadow_mcp",
  "destructive_tool",
  "cli_destructive",
  "prompt_injection",
  "custom",
]);

/** All rule categories in display order */
const ALL_CATEGORIES: RuleCategory[] = [
  "secrets",
  ...PRESIDIO_CATEGORIES,
  "shadow_mcp",
  "destructive_tool",
  "cli_destructive",
  "prompt_injection",
  "off_policy",
];

/** Categories whose source the server rejects with action=block; the form
 * must force flag when any of these are selected. Mirrors validateSourceAction
 * in server/internal/risk/impl.go. */
const FLAG_ONLY_CATEGORIES: Set<RuleCategory> = new Set([
  "destructive_tool",
  "cli_destructive",
]);

/** Steps in the guided standard-policy creation/edit flow. Mirrors the
 *  enterprise onboarding wizard (left rail + paged content). */
const POLICY_WIZARD_STEPS: Step[] = [
  {
    id: "detect",
    title: "Detect",
    description: "What to scan for",
    badge: "Required",
  },
  {
    id: "sensitivity",
    title: "Sensitivity",
    description: "Detection confidence",
    badge: "Optional",
  },
  {
    id: "scope",
    title: "Scope",
    description: "Where it applies",
    badge: "Optional",
  },
  {
    id: "action",
    title: "Action",
    description: "What happens on a match",
    badge: "Required",
  },
  { id: "review", title: "Review", description: "Name & enable" },
];

/** Prompt-policy flow: same staged shell, but step 0 is the guardrail prompt
 *  (+ advanced judge config) instead of detectors. */
const PROMPT_WIZARD_STEPS: Step[] = [
  {
    id: "guardrail",
    title: "Guardrail",
    description: "What to catch, in plain language",
    badge: "Required",
  },
  POLICY_WIZARD_STEPS[2]!, // scope
  POLICY_WIZARD_STEPS[3]!, // action
  POLICY_WIZARD_STEPS[4]!, // review
];

/** Shared wizard chrome: the left step rail + the paged content column. The
 *  footer (Back/Continue/Create) lives in the page shell, driven by the parent. */
function WizardShell({
  steps,
  currentStep,
  setCurrentStep,
  children,
}: {
  steps: Step[];
  currentStep: number;
  setCurrentStep: (v: number) => void;
  children: ReactNode;
}) {
  return (
    <div className="flex gap-8">
      <div className="w-44 flex-shrink-0">
        <OnboardingStepper
          steps={steps}
          currentStep={currentStep}
          onStepClick={(i) => setCurrentStep(i)}
          allowJumpAhead
        />
      </div>
      <div className="min-w-0 flex-1 space-y-6">{children}</div>
    </div>
  );
}

function WizardStepHeading({
  title,
  description,
}: {
  title: string;
  description: string;
}) {
  return (
    <div>
      <h3 className="text-base font-semibold">{title}</h3>
      <p className="text-muted-foreground text-sm">{description}</p>
    </div>
  );
}

function SummaryRow({ label, chips }: { label: string; chips: string[] }) {
  return (
    <div className="flex items-start justify-between gap-3 px-4 py-2.5 text-sm">
      <span className="text-muted-foreground">{label}</span>
      <div className="flex flex-wrap justify-end gap-1">
        {chips.map((chip) => (
          <span
            key={chip}
            className="bg-muted rounded-full px-2 py-0.5 text-xs"
          >
            {chip}
          </span>
        ))}
      </div>
    </div>
  );
}

/** Built-in detectors that run at the category level and have no individual
 *  sub-rules in DETECTION_RULES (their rule list is intentionally empty).
 *  Selecting one of these is enough to enable the policy on its own. */
const CATEGORY_LEVEL_DETECTORS: Set<RuleCategory> = new Set([
  "prompt_injection",
  "shadow_mcp",
  "destructive_tool",
  "cli_destructive",
]);

/** One built-in detector as a toggleable card (Detect step). "Customize" opens
 *  a side-sheet to pick which rules in the category are active. */
function DetectorCard({
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
}) {
  const meta = RULE_CATEGORY_META[category];
  const available = AVAILABLE_CATEGORIES.has(category);
  const rules = DETECTION_RULES[category].filter((r) => !r.hidden);
  const customizable = available && rules.length > 1;
  const enabledCount = rules.filter((r) => !disabledRules.has(r.id)).length;
  const customized = selected && enabledCount < rules.length;
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
function CustomizeRulesSheet({
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
}) {
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

function RuleToggleRow({
  rule,
  checked,
  onToggle,
}: {
  rule: DetectionRule;
  checked: boolean;
  onToggle: (on: boolean) => void;
}) {
  return (
    <div className="hover:bg-muted flex items-center justify-between gap-3 rounded-md px-2 py-2 text-sm">
      <span className="min-w-0 truncate">{rule.title}</span>
      <Switch checked={checked} onCheckedChange={onToggle} />
    </div>
  );
}

type PolicyKind = "risk" | "prompt";
type PolicyAudienceType = "everyone" | "targeted";
type PolicyAudienceChoice = "everyone" | "users" | "roles";

type PolicyRow = { kind: PolicyKind; policy: RiskPolicy };

const USER_SEARCH_RESULT_LIMIT = 10;

const TOOL_CALL_MESSAGE_TYPES = new Set<PolicyMessageType>([
  "tool_request",
  "tool_response",
]);

const ALL_POLICY_MESSAGE_TYPES = Object.keys(
  POLICY_MESSAGE_TYPE_META,
) as Array<PolicyMessageType>;

/** Derive selected categories from a policy's sources + presidioEntities.
 *
 * DETECTION_RULES.id is the canonical `pii.<snake_case>` form; the wire format
 * stored on the policy is the UPPER_SNAKE entity name Presidio speaks. We
 * translate at this boundary so callers never see the wire format. */
function policyToCategories(
  sources: string[],
  presidioEntities?: string[],
): Set<RuleCategory> {
  const cats = new Set<RuleCategory>();
  if (sources.includes("gitleaks")) cats.add("secrets");
  if (sources.includes("shadow_mcp")) cats.add("shadow_mcp");
  if (sources.includes("destructive_tool")) cats.add("destructive_tool");
  if (sources.includes("cli_destructive")) cats.add("cli_destructive");
  if (sources.includes("prompt_injection")) cats.add("prompt_injection");
  for (const cat of PRESIDIO_CATEGORIES) {
    const wireEntities = DETECTION_RULES[cat].map((r) =>
      ruleIdToPresidioEntity(r.id),
    );
    if (wireEntities.some((id) => presidioEntities?.includes(id))) {
      cats.add(cat);
    }
  }
  return cats;
}

/** Derive sources, presidioEntities, promptInjectionRules, and disabledRules
 * from selected categories + per-rule disable set.
 *
 * - `sources` selects which scanners run (category-level).
 * - `presidioEntities` (UPPER_SNAKE) narrows the Presidio query to only the
 *   entities the user has enabled across selected presidio-backed categories.
 *   Rules in `disabledRules` are omitted from this list so the scanner is
 *   never even asked about them.
 * - `disabledRules` (canonical ids like `secret.aws_access_token`) is the
 *   per-rule allowlist applied post-scan for sources without entity-level
 *   query support (gitleaks). It also serves as a redundancy net for
 *   presidio in case of API drift.
 * - `promptInjectionRules` stays empty for backward compatibility — whether
 *   the L1 LLM judge runs on top of the L0 heuristics is chosen per-org via a
 *   feature flag, not by the policy author. */
function categoriesToPayload(
  cats: Set<RuleCategory>,
  disabledRules: Set<string>,
  pinnedHidden: Set<string> = new Set(),
) {
  const sources: string[] = [];
  const presidioEntities: string[] = [];
  const promptInjectionRules: string[] = [];

  if (cats.has("secrets")) sources.push("gitleaks");
  if (cats.has("shadow_mcp")) sources.push("shadow_mcp");
  if (cats.has("destructive_tool")) sources.push("destructive_tool");
  if (cats.has("cli_destructive")) sources.push("cli_destructive");
  if (cats.has("prompt_injection")) sources.push("prompt_injection");
  for (const cat of PRESIDIO_CATEGORIES) {
    if (cats.has(cat)) {
      for (const rule of DETECTION_RULES[cat]) {
        if (disabledRules.has(rule.id)) continue;
        // Hidden rules (deprecated / unreliable upstream) are never newly
        // serialized into the Presidio query just because their category is
        // selected. We only keep one if the policy being edited already
        // pinned it, so an edit round-trips without silently dropping it.
        if ("hidden" in rule && rule.hidden && !pinnedHidden.has(rule.id)) {
          continue;
        }
        presidioEntities.push(ruleIdToPresidioEntity(rule.id));
      }
    }
  }
  if (presidioEntities.length > 0) sources.push("presidio");

  // Persist disabled ids only for currently-selected categories. If a user
  // unselects a category they shouldn't carry over its per-rule overrides.
  const persistedDisabled: string[] = [];
  for (const cat of cats) {
    for (const rule of DETECTION_RULES[cat] ?? []) {
      if (disabledRules.has(rule.id)) persistedDisabled.push(rule.id);
    }
  }

  return {
    sources,
    presidioEntities,
    promptInjectionRules,
    disabledRules: persistedDisabled,
  };
}

/** Canonical ids of hidden rules an existing policy already pins via its
 *  presidioEntities. Lets an edit preserve a deprecated entity the policy
 *  carried before it was hidden, without ever newly adding one. */
function pinnedHiddenRuleIds(presidioEntities?: string[]): Set<string> {
  const pinned = new Set<string>();
  if (!presidioEntities) return pinned;
  for (const cat of PRESIDIO_CATEGORIES) {
    for (const rule of DETECTION_RULES[cat]) {
      if (
        "hidden" in rule &&
        rule.hidden &&
        presidioEntities.includes(ruleIdToPresidioEntity(rule.id))
      ) {
        pinned.add(rule.id);
      }
    }
  }
  return pinned;
}

/** Map sources to display categories for the table row badges. */
function sourcesToCategories(
  sources: string[],
  presidioEntities?: string[],
): RuleCategory[] {
  return [...policyToCategories(sources, presidioEntities)];
}

function policyMessageTypesForForm(
  messageTypes?: string[],
): Set<PolicyMessageType> {
  if (!messageTypes?.length) {
    return new Set(ALL_POLICY_MESSAGE_TYPES);
  }

  return new Set(
    messageTypes.filter((type): type is PolicyMessageType =>
      ALL_POLICY_MESSAGE_TYPES.includes(type as PolicyMessageType),
    ),
  );
}

function policyMessageTypesForPayload(
  selectedMessageTypes: Set<PolicyMessageType>,
): PolicyMessageType[] {
  const orderedTypes = ALL_POLICY_MESSAGE_TYPES.filter((type) =>
    selectedMessageTypes.has(type),
  );
  if (orderedTypes.length === ALL_POLICY_MESSAGE_TYPES.length) {
    return [];
  }
  return orderedTypes;
}

function policyMessageTypesForDisplay(
  messageTypes?: string[],
): PolicyMessageType[] {
  return [...policyMessageTypesForForm(messageTypes)];
}

function policyAudienceSummary(row: PolicyRow): string {
  if (row.kind === "prompt") {
    return "Everyone";
  }
  if (row.policy.audienceType !== "targeted") {
    return "Everyone";
  }

  const count = row.policy.audiencePrincipalUrns.length;
  if (count === 1) {
    return "1 target";
  }
  return `${count} targets`;
}

function policyAudienceChoiceForSelection(
  audienceType: PolicyAudienceType,
  principalUrns: Set<string>,
): PolicyAudienceChoice {
  if (audienceType === "everyone") {
    return "everyone";
  }

  const hasUser = [...principalUrns].some((urn) => urn.startsWith("user:"));
  if (hasUser) {
    return "users";
  }

  const hasRole = [...principalUrns].some((urn) => urn.startsWith("role:"));
  return hasRole ? "roles" : "users";
}

function filterAudiencePrincipalsForChoice(
  principalUrns: Set<string>,
  choice: PolicyAudienceChoice,
): Set<string> {
  if (choice === "everyone") {
    return new Set<string>();
  }

  const prefix = choice === "users" ? "user:" : "role:";
  return new Set([...principalUrns].filter((urn) => urn.startsWith(prefix)));
}

function memberDisplayName(member: AccessMember): string {
  return member.name || member.email;
}

function memberInitials(member: Pick<AccessMember, "email" | "name">): string {
  const source = member.name.trim() || member.email.trim();
  const initials = source
    .split(/\s+/)
    .map((part) => part[0])
    .join("")
    .toUpperCase()
    .slice(0, 2);

  return initials || "?";
}

function memberMatchesSearch(member: AccessMember, search: string): boolean {
  const normalizedSearch = search.trim().toLowerCase();
  if (!normalizedSearch) {
    return false;
  }

  return (
    member.name.toLowerCase().includes(normalizedSearch) ||
    member.email.toLowerCase().includes(normalizedSearch)
  );
}

function compareMembersByName(a: AccessMember, b: AccessMember): number {
  return memberDisplayName(a).localeCompare(memberDisplayName(b));
}

function compareRolesByName(a: Role, b: Role): number {
  return a.name.localeCompare(b.name);
}

function hasOnlyToolCallMessageTypes(types: Set<PolicyMessageType>): boolean {
  return (
    types.size === TOOL_CALL_MESSAGE_TYPES.size &&
    [...types].every((type) => TOOL_CALL_MESSAGE_TYPES.has(type))
  );
}

function messageTypesSummary(
  selectedMessageTypes: Set<PolicyMessageType>,
): string {
  if (selectedMessageTypes.size === ALL_POLICY_MESSAGE_TYPES.length) {
    return "All types";
  }

  if (hasOnlyToolCallMessageTypes(selectedMessageTypes)) {
    return "Tool Calls";
  }

  if (
    selectedMessageTypes.size === 1 &&
    selectedMessageTypes.has("tool_request")
  ) {
    return "Tool Requests";
  }

  return `${selectedMessageTypes.size} of ${ALL_POLICY_MESSAGE_TYPES.length} types selected`;
}

function truncatePrompt(prompt: string, maxLength = 60): string {
  const singleLine = prompt.trim().replace(/\s+/g, " ");
  if (singleLine.length <= maxLength) {
    return singleLine;
  }
  return `${singleLine.slice(0, maxLength - 1)}…`;
}

function promptTemplateNameForInstruction(prompt: string): string | undefined {
  return PROMPT_POLICY_TEMPLATES.find((template) => template.prompt === prompt)
    ?.name;
}

function isPromptPolicy(policy: RiskPolicy): boolean {
  return policy.policyType === "prompt_based";
}

/** The wizard flow kind for the current URL/editing state. When editing, the
 *  policy dictates the kind; otherwise the `?create=` param does. */
function policyKindForForm(
  editingPolicy: RiskPolicy | null,
  createParam: string | null,
): PolicyKind {
  if (editingPolicy) return isPromptPolicy(editingPolicy) ? "prompt" : "risk";
  return createParam === "prompt" ? "prompt" : "risk";
}

/** Id of the first step for a wizard flow, used to seed the `?step=` param when
 *  opening the flow. Derived from the step lists so the two never drift. */
function firstStepId(kind: PolicyKind): string {
  const steps = kind === "prompt" ? PROMPT_WIZARD_STEPS : POLICY_WIZARD_STEPS;
  return steps[0]!.id;
}

function promptPolicyName(prompt: string): string {
  return prompt.trim().replace(/\s+/g, " ").slice(0, 60) || "Prompt Policy";
}

/** Example scope CEL snippets offered beneath the include field — narrow a
 *  policy to a subset of messages. */
const SCOPE_INCLUDE_CEL_EXAMPLES: { label: string; expr: string }[] = [
  {
    label: "Only a GitHub server",
    expr: 'tools.exists(t, t.server.matchExact("github"))',
  },
  {
    label: "Production prompts",
    expr: 'prompt.matchText("production")',
  },
  {
    label: "Delete-style tools",
    expr: 'tools.exists(t, t.function.matchGlob("*delete*"))',
  },
];

/** Example scope CEL snippets offered beneath the exempt field — take matching
 *  messages out of the policy entirely (an allowlist). */
const SCOPE_EXEMPT_CEL_EXAMPLES: { label: string; expr: string }[] = [
  {
    label: "Read-only tools",
    expr: 'tools.exists(t, t.function.matchGlob("*get*") || t.function.matchGlob("*list*"))',
  },
  {
    label: "A safelisted server",
    expr: 'tools.exists(t, t.server.matchExact("internal-docs"))',
  },
];

export default function PolicyCenter(): JSX.Element {
  return (
    <RequireScope scope="org:admin" level="page">
      <PolicyCenterContent />
    </RequireScope>
  );
}

function PolicyCenterContent() {
  const queryClient = useQueryClient();
  const telemetry = useTelemetry();
  const { data, isLoading } = useRiskListPolicies();
  const nlEnabled = telemetry.isFeatureEnabled("gram-prompt-policies") ?? false;

  const policyRows = useMemo(
    (): PolicyRow[] =>
      (data?.policies ?? [])
        .filter((policy) => nlEnabled || !isPromptPolicy(policy))
        .map((policy) => {
          const kind: PolicyKind = isPromptPolicy(policy) ? "prompt" : "risk";
          return { kind, policy };
        })
        .sort(
          (a, b) => b.policy.createdAt.getTime() - a.policy.createdAt.getTime(),
        ),
    [data?.policies, nlEnabled],
  );

  const { customRules } = useDetectionRulesStore();

  const [editingPolicy, setEditingPolicy] = useState<RiskPolicy | null>(null);
  const [formName, setFormName] = useState("");
  const [formEnabled, setFormEnabled] = useState(true);
  const [formPromptInstruction, setFormPromptInstruction] = useState("");
  const [selectedCategories, setSelectedCategories] = useState<
    Set<RuleCategory>
  >(new Set<RuleCategory>());
  const [disabledRules, setDisabledRules] = useState<Set<string>>(new Set());
  const [selectedCustomRuleIds, setSelectedCustomRuleIds] = useState<
    Set<string>
  >(new Set<string>());
  // Fine-grained applicability predicates (scope_include / scope_exempt):
  // a CEL expression scoping the policy, and one taking matching messages out of
  // it. Empty => rely on the coarse message_types knob.
  const [scopeInclude, setScopeInclude] = useState("");
  const [scopeExempt, setScopeExempt] = useState("");
  // Scope is defined EITHER by the coarse message-type cards OR by a custom CEL
  // include predicate — a mutex. message_types is kept either way but only sent
  // (and only required) in "messageTypes" mode.
  const [scopeMode, setScopeMode] = useState<"messageTypes" | "cel">(
    "messageTypes",
  );
  const [selectedMessageTypes, setSelectedMessageTypes] = useState<
    Set<PolicyMessageType>
  >(new Set(ALL_POLICY_MESSAGE_TYPES));
  const [formAction, setFormAction] = useState<PolicyAction>("flag");
  const [formAutoName, setFormAutoName] = useState(true);
  const [formUserMessage, setFormUserMessage] = useState("");
  // Empty string => use the server's default judge model (see JUDGE_MODEL_OPTIONS).
  const [formModel, setFormModel] = useState("");
  // Judge sampling temperature. Defaults to the benchmark's deterministic
  // setting (DEFAULT_JUDGE_TEMPERATURE); only persisted when changed from it.
  const [formTemperature, setFormTemperature] = useState(
    DEFAULT_JUDGE_TEMPERATURE,
  );
  // Per-policy Presidio detection-sensitivity threshold for standard policies.
  // Only persisted when at least one Presidio category is active.
  const [formPresidioThreshold, setFormPresidioThreshold] = useState<number>(
    DEFAULT_PRESIDIO_THRESHOLD,
  );
  // Fail-open (true) is the server default: allow the message when the judge errors.
  const [formFailOpen, setFormFailOpen] = useState(true);
  const [formAudienceType, setFormAudienceType] =
    useState<PolicyAudienceType>("everyone");
  const [selectedAudiencePrincipalUrns, setSelectedAudiencePrincipalUrns] =
    useState<Set<string>>(new Set<string>());

  const [runPanelPolicy, setRunPanelPolicy] = useState<RiskPolicy | null>(null);

  const [activeTab, setActiveTab] = useState<"policies" | "exclusions">(
    "policies",
  );
  const [exclusionSheet, setExclusionSheet] =
    useState<ExclusionSheetState | null>(null);

  const navigate = useNavigate();

  // The whole wizard location lives in the URL so native browser Back/Forward
  // page through the flow (AIS-238). `policy` opens an existing policy for edit
  // (also the command-palette deep link, since policies have no per-item
  // route); `create` opens the new-policy flow ("type" chooser, or a chosen
  // kind); `step` is the active step id. Forward moves (open, Continue, stepper
  // jump) push a history entry; closing clears the params via a replace.
  const [nav, setNav] = useQueryStates({
    policy: parseAsString,
    create: parseAsStringLiteral(["type", "risk", "prompt"] as const),
    step: parseAsString,
  });
  const openedPolicyRef = useRef<string | null>(null);

  // The sheet is open whenever we're editing a policy or in the create flow.
  const sheetOpen = editingPolicy !== null || nav.create !== null;
  const formPolicyKind = policyKindForForm(editingPolicy, nav.create);

  const closeSheet = useCallback(() => {
    setEditingPolicy(null);
    openedPolicyRef.current = null;
    void setNav({ policy: null, create: null, step: null });
  }, [setNav]);

  // The create/edit view is a focused full-screen card (not a Dialog), so wire
  // Escape to close it. Skip when a layered sub-sheet (e.g. the category
  // Customize sheet) already handled Escape — it calls preventDefault on dismiss.
  useEffect(() => {
    if (!sheetOpen) return;
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key !== "Escape" || e.defaultPrevented) return;
      closeSheet();
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [sheetOpen, closeSheet]);

  // While the create/edit card is open it owns the whole viewport, so hide the
  // floating assistant dock for the duration (it would otherwise float on top).
  const { registerDockHide } = useInsightsState();
  useLayoutEffect(() => {
    if (!sheetOpen) return;
    return registerDockHide();
  }, [sheetOpen, registerDockHide]);

  const invalidate = useCallback(() => {
    void invalidateAllRiskListPolicies(queryClient);
    void invalidateAllRiskPoliciesStatus(queryClient);
  }, [queryClient]);

  const createMutation = useRiskCreatePolicyMutation({
    onSuccess: () => {
      invalidate();
      closeSheet();
    },
  });

  const updateMutation = useRiskPoliciesUpdateMutation({
    onSuccess: () => {
      invalidate();
      closeSheet();
    },
  });

  const deleteMutation = useRiskPoliciesDeleteMutation({
    onSuccess: invalidate,
  });

  const handleCreate = (kind?: PolicyKind) => {
    const nextKind = kind ?? "risk";
    setEditingPolicy(null);
    openedPolicyRef.current = null;
    setFormName("");
    setFormEnabled(true);
    setFormPromptInstruction("");
    setSelectedCategories(new Set<RuleCategory>());
    setDisabledRules(new Set());
    setSelectedCustomRuleIds(new Set<string>());
    setScopeInclude("");
    setScopeExempt("");
    setScopeMode("messageTypes");
    setSelectedMessageTypes(new Set(ALL_POLICY_MESSAGE_TYPES));
    setFormAction("flag");
    setFormAutoName(true);
    setFormUserMessage("");
    setFormModel("");
    setFormTemperature(DEFAULT_JUDGE_TEMPERATURE);
    setFormPresidioThreshold(DEFAULT_PRESIDIO_THRESHOLD);
    setFormFailOpen(true);
    setFormAudienceType("everyone");
    setSelectedAudiencePrincipalUrns(new Set<string>());
    // Open the type chooser when NL policies are enabled and no kind was
    // forced; otherwise jump straight to the chosen kind's first step. Pushed
    // so the browser Back button closes the freshly-opened flow.
    if (!kind && nlEnabled) {
      void setNav(
        { policy: null, create: "type", step: null },
        { history: "push" },
      );
    } else {
      void setNav(
        { policy: null, create: nextKind, step: firstStepId(nextKind) },
        { history: "push" },
      );
    }
  };

  const handleChoosePolicyKind = (kind: PolicyKind) => {
    // Reset the kind-specific defaults, then advance from the chooser into the
    // first step of the chosen flow (pushed so Back returns to the chooser).
    setSelectedMessageTypes(new Set(ALL_POLICY_MESSAGE_TYPES));
    setFormAction("flag");
    setFormAutoName(true);
    void setNav({ create: kind, step: firstStepId(kind) }, { history: "push" });
  };

  // Memoized so the deep-link effect below can depend on it without re-running
  // every render. Body references only module-level helpers + stable setters
  // (setNav is stable), so listing setNav is enough.
  const handleEdit = useCallback(
    (
      policy: RiskPolicy,
      // Deep-link hydration passes `history: "replace"` (the `?policy=` entry
      // already exists, so pushing would add a duplicate Back stop) and the
      // step already in the URL, so a reload keeps its place.
      opts?: { history?: "push" | "replace"; step?: string | null },
    ) => {
      const isPrompt = isPromptPolicy(policy);
      const kind: PolicyKind = isPrompt ? "prompt" : "risk";
      const history = opts?.history ?? "push";
      const step = opts?.step ?? firstStepId(kind);
      // Mark the deep-link effect as handled for this id so it doesn't re-open
      // the sheet when we set `?policy=` below.
      openedPolicyRef.current = policy.id;
      setEditingPolicy(policy);
      setFormName(policy.name);
      setFormEnabled(policy.enabled);
      // Scope CEL applies to both kinds; load it before the kind branch.
      const loadedInclude = policy.scopeInclude ?? "";
      setScopeInclude(loadedInclude);
      setScopeExempt(policy.scopeExempt ?? "");
      setScopeMode(loadedInclude.trim() !== "" ? "cel" : "messageTypes");
      if (isPrompt) {
        setFormPromptInstruction(policy.prompt ?? "");
        setSelectedMessageTypes(policyMessageTypesForForm(policy.messageTypes));
        setFormAction((policy.action as PolicyAction) ?? "flag");
        setFormAutoName(policy.autoName ?? true);
        setFormUserMessage(policy.userMessage ?? "");
        setFormModel(policy.modelConfig?.model ?? "");
        setFormTemperature(
          policy.modelConfig?.temperature ?? DEFAULT_JUDGE_TEMPERATURE,
        );
        setFormFailOpen(policy.modelConfig?.failOpen ?? true);
        setFormAudienceType("everyone");
        setSelectedAudiencePrincipalUrns(new Set<string>());
        void setNav({ policy: policy.id, create: null, step }, { history });
        return;
      }
      setFormPromptInstruction("");
      const customRuleIds = policy.customRuleIds ?? [];
      const categories = policyToCategories(
        policy.sources,
        policy.presidioEntities,
      );
      if (customRuleIds.length > 0) {
        categories.add("custom");
      }
      setSelectedCategories(categories);
      setFormPresidioThreshold(
        policy.presidioScoreThreshold ?? DEFAULT_PRESIDIO_THRESHOLD,
      );
      setDisabledRules(new Set(policy.disabledRules ?? []));
      setSelectedCustomRuleIds(new Set<string>(customRuleIds));
      setSelectedMessageTypes(policyMessageTypesForForm(policy.messageTypes));
      setFormAction((policy.action as PolicyAction) ?? "flag");
      setFormAutoName(policy.autoName ?? true);
      setFormUserMessage(policy.userMessage ?? "");
      const audienceType = policy.audienceType ?? "everyone";
      setFormAudienceType(audienceType);
      setSelectedAudiencePrincipalUrns(
        audienceType === "targeted"
          ? new Set<string>(policy.audiencePrincipalUrns ?? [])
          : new Set<string>(),
      );
      void setNav({ policy: policy.id, create: null, step }, { history });
    },
    [setNav],
  );

  // Open the deep-linked policy once its data has loaded. Guarded by a ref so it
  // fires once per id (not on every policies re-fetch). The ref is marked as
  // handled even when the id doesn't resolve, so a stale/invalid id doesn't
  // re-trigger the lookup on every subsequent `data` change.
  useEffect(() => {
    if (!nav.policy || isLoading) return;
    if (openedPolicyRef.current === nav.policy) return;
    // Read from the stable react-query `data` (not the per-render `policies`
    // array) so the effect doesn't re-run every render.
    const policy = data?.policies?.find((p) => p.id === nav.policy);
    openedPolicyRef.current = nav.policy;
    if (policy) {
      // Hydrating from the URL: rewrite the current entry rather than pushing a
      // duplicate, and keep whatever step the URL already carries.
      handleEdit(policy, { history: "replace", step: nav.step });
    }
  }, [nav.policy, nav.step, isLoading, data, handleEdit]);

  // When the URL stops pointing at a policy — e.g. the user pressed the browser
  // Back button out of an edit session — drop the loaded policy so the sheet
  // closes. Open paths set `editingPolicy` and `?policy=` in the same render,
  // so this can't race an open.
  useEffect(() => {
    if (nav.policy === null && editingPolicy !== null) {
      setEditingPolicy(null);
      openedPolicyRef.current = null;
    }
  }, [nav.policy, editingPolicy]);

  const handleSave = () => {
    // Fine-grained scope predicates (both kinds). The include applies only in
    // CEL scope mode; the exempt is additive and always sent. On update we
    // always send a value (empty string clears) so the omit-to-preserve impl
    // can replace it; on create we omit when empty.
    const includeCel = scopeMode === "cel" ? scopeInclude.trim() : "";
    const exemptCel = scopeExempt.trim();
    const applicationUpdate = {
      scopeInclude: includeCel,
      scopeExempt: exemptCel,
    };
    const applicationCreate = {
      ...(includeCel ? { scopeInclude: includeCel } : {}),
      ...(exemptCel ? { scopeExempt: exemptCel } : {}),
    };
    if (formPolicyKind === "prompt") {
      const prompt = formPromptInstruction.trim();
      const name = formAutoName ? promptPolicyName(prompt) : formName;
      // Persist model_config only when it diverges from defaults (or already
      // existed), and include each field only when non-default, so an unset
      // config stays NULL rather than churning the policy version on every edit.
      const temperatureIsCustom = formTemperature !== DEFAULT_JUDGE_TEMPERATURE;
      const hasModelConfig =
        !!editingPolicy?.modelConfig ||
        !!formModel ||
        temperatureIsCustom ||
        !formFailOpen;
      const modelConfig = hasModelConfig
        ? {
            ...(formModel ? { model: formModel } : {}),
            ...(temperatureIsCustom ? { temperature: formTemperature } : {}),
            failOpen: formFailOpen,
          }
        : undefined;
      const userMessagePayload = formUserMessage.trim()
        ? { userMessage: formUserMessage }
        : {};
      // In CEL scope mode the include predicate is the sole scope: send no
      // message-type filter so a stale subset can't intersect it (the two are a
      // mutex). The selection is preserved in form state for a mode switch-back.
      const promptMessageTypes =
        scopeMode === "cel"
          ? []
          : policyMessageTypesForPayload(selectedMessageTypes);
      if (editingPolicy) {
        updateMutation.mutate({
          request: {
            updateRiskPolicyRequestBody: {
              id: editingPolicy.id,
              name,
              enabled: formEnabled,
              prompt,
              messageTypes: promptMessageTypes,
              action: formAction,
              autoName: formAutoName,
              ...applicationUpdate,
              ...(modelConfig ? { modelConfig } : {}),
              ...userMessagePayload,
            },
          },
        });
      } else {
        createMutation.mutate({
          request: {
            createRiskPolicyRequestBody: {
              name,
              policyType: "prompt_based",
              enabled: formEnabled,
              prompt,
              messageTypes: promptMessageTypes,
              action: formAction,
              autoName: formAutoName,
              ...applicationCreate,
              ...(modelConfig ? { modelConfig } : {}),
              ...userMessagePayload,
            },
          },
        });
      }
      return;
    }

    // CEL scope mode replaces the message-type selection; send no message-type
    // filter so a stale subset can't intersect the include predicate.
    const messageTypes =
      scopeMode === "cel"
        ? []
        : policyMessageTypesForPayload(selectedMessageTypes);
    const {
      sources,
      presidioEntities,
      promptInjectionRules,
      disabledRules: payloadDisabled,
    } = categoriesToPayload(
      selectedCategories,
      disabledRules,
      pinnedHiddenRuleIds(
        editingPolicy ? editingPolicy.presidioEntities : undefined,
      ),
    );
    const action =
      sources.includes("destructive_tool") && formAction === "block"
        ? "flag"
        : formAction;
    const audiencePrincipalUrns =
      formAudienceType === "targeted" ? [...selectedAudiencePrincipalUrns] : [];
    // Only persist the Presidio threshold when a Presidio category is active, so
    // non-Presidio policies don't carry a stray threshold value.
    const presidioActive = PRESIDIO_CATEGORIES.some((c) =>
      selectedCategories.has(c),
    );
    if (editingPolicy) {
      updateMutation.mutate({
        request: {
          updateRiskPolicyRequestBody: {
            id: editingPolicy.id,
            name: formName,
            enabled: formEnabled,
            sources,
            presidioEntities,
            promptInjectionRules,
            disabledRules: payloadDisabled,
            customRuleIds: [...selectedCustomRuleIds],
            messageTypes,
            ...applicationUpdate,
            action,
            audienceType: formAudienceType,
            audiencePrincipalUrns,
            autoName: formAutoName,
            userMessage: formUserMessage,
            ...(presidioActive
              ? { presidioScoreThreshold: formPresidioThreshold }
              : {}),
          },
        },
      });
    } else {
      createMutation.mutate({
        request: {
          createRiskPolicyRequestBody: {
            ...(formAutoName ? {} : { name: formName }),
            enabled: formEnabled,
            sources,
            presidioEntities,
            promptInjectionRules,
            disabledRules: payloadDisabled,
            customRuleIds: [...selectedCustomRuleIds],
            messageTypes,
            ...applicationCreate,
            action,
            audienceType: formAudienceType,
            audiencePrincipalUrns,
            autoName: formAutoName,
            ...(formUserMessage.trim() ? { userMessage: formUserMessage } : {}),
            ...(presidioActive
              ? { presidioScoreThreshold: formPresidioThreshold }
              : {}),
          },
        },
      });
    }
  };

  const handleDelete = (row: PolicyRow) => {
    deleteMutation.mutate({ request: { id: row.policy.id } });
  };

  const handleToggle = (policy: RiskPolicy, enabled: boolean) => {
    updateMutation.mutate({
      request: {
        updateRiskPolicyRequestBody: {
          id: policy.id,
          name: policy.name,
          enabled,
          messageTypes: policy.messageTypes ?? [],
        },
      },
    });
  };

  // Empty state for the Policies tab only. It must NOT short-circuit the whole
  // page, otherwise the Exclusions tab (and global exclusions) would be
  // unreachable for projects that have no policies yet.
  const policiesEmptyState = (
    <div className="bg-muted/20 flex flex-col items-center justify-center rounded-xl border border-dashed px-8 py-16">
      <div className="bg-muted/50 mb-4 flex h-12 w-12 items-center justify-center rounded-full">
        <Shield className="text-muted-foreground h-6 w-6" />
      </div>
      <Type variant="subheading" className="mb-1">
        No Risk Policies
      </Type>
      <Type small muted className="mb-4 max-w-md text-center">
        Risk policies scan your chat messages for secrets and sensitive data.
        Create your first policy to get started.
      </Type>
      <Button
        onClick={() => {
          const {
            sources,
            presidioEntities,
            promptInjectionRules,
            disabledRules: payloadDisabled,
          } = categoriesToPayload(
            new Set<RuleCategory>(["secrets", "pii"]),
            new Set(),
          );
          createMutation.mutate({
            request: {
              createRiskPolicyRequestBody: {
                autoName: true,
                enabled: true,
                sources,
                presidioEntities,
                promptInjectionRules,
                disabledRules: payloadDisabled,
                customRuleIds: [],
              },
            },
          });
        }}
        disabled={createMutation.isPending}
      >
        <Button.Text>
          {createMutation.isPending ? (
            <>
              <Loader2 className="mr-2 h-4 w-4 animate-spin" />
              Creating...
            </>
          ) : (
            "Get Started"
          )}
        </Button.Text>
      </Button>
    </div>
  );

  const enabledPolicies = policyRows.filter((r) => r.policy.enabled);
  const insightsContext = [
    "Page: Policy Center.",
    `Total policies: ${policyRows.length}, enabled: ${enabledPolicies.length}.`,
    `Policy actions: ${policyRows.map((r) => `${r.policy.name} (${r.policy.action})`).join(", ") || "none"}.`,
    "Available risk tools: listRiskPolicies, getRiskPolicy, getRiskPolicyStatus, listRiskResultsForAgent (finding-level with match redaction), listRiskResultsByChat, listShadowMCPApprovals.",
    "Never echo match_redacted values verbatim. Refer to findings by rule_id and source.",
  ].join(" ");

  const dimIfDisabled = (row: PolicyRow) =>
    row.policy.enabled ? "" : "opacity-50";

  const policyColumns: Column<PolicyRow>[] = [
    {
      key: "name",
      header: "Name",
      width: "1fr",
      render: (row) => (
        <span
          className={cn(
            "flex min-w-0 items-center gap-1.5 font-medium",
            dimIfDisabled(row),
          )}
        >
          <span className="truncate">{row.policy.name}</span>
          {row.kind === "prompt" && (
            <SimpleTooltip tooltip="Prompt-based policy">
              <Sparkles
                aria-label="Prompt-based policy"
                className="text-muted-foreground h-3.5 w-3.5 shrink-0"
              />
            </SimpleTooltip>
          )}
        </span>
      ),
    },
    {
      key: "action",
      header: "Action",
      width: "0.5fr",
      render: (row) => (
        <span className={cn("inline-flex", dimIfDisabled(row))}>
          <ActionBadge action={(row.policy.action as PolicyAction) ?? "flag"} />
        </span>
      ),
    },
    {
      key: "sources",
      header: nlEnabled ? "Categories / Prompt" : "Categories",
      width: "2fr",
      render: (row) => {
        if (row.kind === "prompt") {
          const prompt = row.policy.prompt ?? "";
          return (
            <SimpleTooltip tooltip={prompt}>
              <span
                className={cn(
                  "text-muted-foreground block max-w-full truncate text-sm italic",
                  dimIfDisabled(row),
                )}
              >
                {truncatePrompt(prompt)}
              </span>
            </SimpleTooltip>
          );
        }

        const riskPolicy = row.policy;
        const categories = sourcesToCategories(
          riskPolicy.sources,
          riskPolicy.presidioEntities,
        );
        if (riskPolicy.customRuleIds?.length) {
          categories.push("custom");
        }

        if (categories.length === 0) {
          return <span className="text-muted-foreground text-sm">—</span>;
        }

        return (
          <span
            className={cn("text-muted-foreground text-sm", dimIfDisabled(row))}
          >
            {categories.map((cat) => RULE_CATEGORY_META[cat].label).join(", ")}
          </span>
        );
      },
    },
    {
      key: "messageTypes",
      header: "Applies To",
      width: "2.1fr",
      render: (row) => {
        const types = policyMessageTypesForDisplay(row.policy.messageTypes);
        const typeSet = new Set(types);
        const tooltip = types
          .map((type) => POLICY_MESSAGE_TYPE_META[type].label)
          .join(", ");

        if (
          typeSet.size === ALL_POLICY_MESSAGE_TYPES.length ||
          hasOnlyToolCallMessageTypes(typeSet)
        ) {
          return (
            <SimpleTooltip tooltip={tooltip}>
              <span
                className={cn(
                  "text-muted-foreground text-sm",
                  dimIfDisabled(row),
                )}
              >
                {messageTypesSummary(typeSet)}
              </span>
            </SimpleTooltip>
          );
        }

        return (
          <span
            className={cn("text-muted-foreground text-sm", dimIfDisabled(row))}
          >
            {types
              .map((type) => POLICY_MESSAGE_TYPE_META[type].label)
              .join(", ")}
          </span>
        );
      },
    },
    {
      key: "audience",
      header: "Audience",
      width: "1fr",
      render: (row) => (
        <span
          className={cn("text-muted-foreground text-sm", dimIfDisabled(row))}
        >
          {policyAudienceSummary(row)}
        </span>
      ),
    },
    {
      key: "enabled",
      header: "Enabled",
      width: "0.5fr",
      render: (row) => (
        <div onClick={(e) => e.stopPropagation()}>
          <Switch
            checked={row.policy.enabled}
            onCheckedChange={(checked) => handleToggle(row.policy, checked)}
          />
        </div>
      ),
    },
    {
      key: "actions",
      header: "",
      width: "0.3fr",
      render: (row) => (
        <div onClick={(e) => e.stopPropagation()}>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button
                variant="tertiary"
                size="sm"
                onClick={(e) => e.stopPropagation()}
              >
                <Button.Icon>
                  <Ellipsis className="h-4 w-4" />
                </Button.Icon>
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuItem
                className="cursor-pointer"
                onSelect={() => {
                  setTimeout(() => handleEdit(row.policy), 0);
                }}
              >
                Edit
              </DropdownMenuItem>
              {row.kind === "risk" && (
                <DropdownMenuItem
                  className="cursor-pointer"
                  onSelect={() => {
                    setTimeout(() => setRunPanelPolicy(row.policy), 0);
                  }}
                >
                  View Progress
                </DropdownMenuItem>
              )}
              <DropdownMenuItem
                className="text-destructive focus:text-destructive cursor-pointer"
                onSelect={() => handleDelete(row)}
              >
                Delete
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      ),
    },
  ];

  const headerAction =
    activeTab === "policies"
      ? { label: "New Policy", onClick: () => handleCreate() }
      : {
          label: "Create Exclusion",
          onClick: () => setExclusionSheet({ mode: "create" }),
        };

  const isChoosingPolicyKind = !editingPolicy && nav.create === "type";

  // Footer behaviour for the guided flow. Both standard and prompt policies now
  // page through steps (Continue, then Create/Update); only the type chooser is
  // exempt.
  const isWizard = sheetOpen && !isChoosingPolicyKind;
  const isRiskWizard = isWizard && formPolicyKind === "risk";
  const wizardSteps =
    formPolicyKind === "prompt" ? PROMPT_WIZARD_STEPS : POLICY_WIZARD_STEPS;
  // The active step is derived from the `?step=` param; an absent/unknown value
  // falls back to the first step. `setWizardStep` writes it back and pushes a
  // history entry so browser Back returns to the previous step.
  const wizardStep = Math.max(
    0,
    wizardSteps.findIndex((s) => s.id === nav.step),
  );
  const setWizardStep = useCallback(
    (index: number) => {
      const id = wizardSteps[index]?.id;
      if (id) void setNav({ step: id }, { history: "push" });
    },
    [wizardSteps, setNav],
  );
  const isLastWizardStep = wizardStep === wizardSteps.length - 1;
  const showWizardContinue = isWizard && !isLastWizardStep;
  const mutationPending = createMutation.isPending || updateMutation.isPending;
  // Scope is satisfied by either a message-type selection or — in CEL mode — a
  // non-empty include expression (the two are a mutex). Compile validity is
  // surfaced inline and enforced by the backend on save.
  const includeCelStatus = useCelStatus(
    scopeMode === "cel" ? scopeInclude : "",
  );
  const exemptCelStatus = useCelStatus(scopeExempt);
  const scopeMissing =
    scopeMode === "messageTypes"
      ? selectedMessageTypes.size === 0
      : scopeInclude.trim() === "";
  // A standard policy needs at least one detector that will actually run: a
  // custom rule, a category-level detector (no sub-rules to enable), or a
  // selected category with at least one of its rules enabled.
  const hasEnabledDetector =
    selectedCustomRuleIds.size > 0 ||
    [...selectedCategories].some(
      (c) =>
        CATEGORY_LEVEL_DETECTORS.has(c) ||
        DETECTION_RULES[c]?.some((r) => !r.hidden && !disabledRules.has(r.id)),
    );
  // Step validation is keyed by step id so it works for both the standard
  // layout (scope at index 2) and the prompt layout (scope at index 1). The
  // "sensitivity" step is Optional and never blocks.
  const currentStepId = wizardSteps[wizardStep]?.id;
  const continueDisabled =
    (currentStepId === "detect" && !hasEnabledDetector) ||
    (currentStepId === "guardrail" && !formPromptInstruction.trim()) ||
    (currentStepId === "scope" && scopeMissing);
  // Block save while a scope expression that will be sent fails to compile.
  const applicationInvalid =
    (scopeMode === "cel" && includeCelStatus.kind === "error") ||
    exemptCelStatus.kind === "error";
  const saveDisabled =
    (formPolicyKind === "prompt" && !formPromptInstruction.trim()) ||
    // A standard policy needs at least one detector or custom rule (the step-0
    // gate, re-checked here since free-jump can skip it).
    (isRiskWizard && !hasEnabledDetector) ||
    (!formAutoName && !formName.trim()) ||
    scopeMissing ||
    applicationInvalid ||
    // A targeted audience needs at least one selected principal.
    (formPolicyKind === "risk" &&
      formAudienceType === "targeted" &&
      selectedAudiencePrincipalUrns.size === 0) ||
    mutationPending;
  const showFooterBack =
    (isWizard && wizardStep > 0) || (!editingPolicy && nlEnabled);
  // Pop the last pushed entry — a previous step, or the type chooser when at
  // step 0 of the create flow. Identical to the browser Back button, which now
  // drives the wizard too.
  const onFooterBack = () => {
    void navigate(-1);
  };

  let sheetTitle = "New Policy";
  let sheetDescription = "Create a policy to scan agent session interactions.";
  if (editingPolicy) {
    sheetTitle = "Edit Policy";
    sheetDescription = "Update this policy configuration.";
  } else if (isChoosingPolicyKind) {
    sheetTitle = "Choose Policy Type";
    sheetDescription =
      "Start with a detector-based policy or define criteria in plain language.";
  } else if (nlEnabled) {
    if (formPolicyKind === "prompt") {
      sheetTitle = "New Prompt-based Policy";
      sheetDescription = "Describe the tool-call behavior you want to detect.";
    } else {
      sheetTitle = "New Policy";
      sheetDescription = "Configure detection rules to scan agent sessions.";
    }
  }

  let policiesBody = (
    <Table
      columns={policyColumns}
      data={policyRows}
      rowKey={(row) => row.policy.id}
      onRowClick={(row) => handleEdit(row.policy)}
    />
  );
  if (isLoading) {
    policiesBody = (
      <div className="flex items-center justify-center py-20">
        <Loader2 className="text-muted-foreground h-5 w-5 animate-spin" />
      </div>
    );
  } else if (policyRows.length === 0) {
    policiesBody = policiesEmptyState;
  }

  const cta = isLoading ? null : (
    <Page.Section.CTA>
      <Button onClick={headerAction.onClick}>
        <Button.LeftIcon>
          <Plus className="mr-2 h-4 w-4" />
        </Button.LeftIcon>
        <Button.Text>{headerAction.label}</Button.Text>
      </Button>
    </Page.Section.CTA>
  );

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <InsightsConfig
          contextInfo={insightsContext}
          suggestions={INSIGHTS_SUGGESTIONS["risk-policies"]}
          title="Policy insights"
          subtitle="Ask about policy status, coverage, and detector capabilities. Match content is redacted before it reaches the assistant."
        />
        <Page.Section>
          <Page.Section.Title stage="beta">Policies</Page.Section.Title>
          <Page.Section.Description>
            Configure policies to detect secrets, sensitive information, and
            prompt-defined risks in agent session interactions.
          </Page.Section.Description>
          {cta}
          <Page.Section.Body>
            <Tabs
              value={activeTab}
              onValueChange={(value) =>
                setActiveTab(value as "policies" | "exclusions")
              }
            >
              <div className="border-b">
                <TabsList className="h-auto justify-start gap-4 rounded-none bg-transparent p-0 text-sm">
                  <PageTabsTrigger value="policies">Policies</PageTabsTrigger>
                  <PageTabsTrigger value="exclusions">
                    Exclusions
                  </PageTabsTrigger>
                </TabsList>
              </div>
              <TabsContent value="policies" className="mt-6">
                {policiesBody}
              </TabsContent>
              <TabsContent value="exclusions" className="mt-6">
                <ExclusionsTab
                  policies={data?.policies ?? []}
                  sheet={exclusionSheet}
                  onSheetChange={setExclusionSheet}
                />
              </TabsContent>
            </Tabs>
          </Page.Section.Body>
        </Page.Section>

        {/* Edit/Create page: a focused full-screen card that covers the nav
         *  sidebar and assistant dock, inset by a small gutter so it reads as
         *  padded rather than full-bleed — mirrors the inset content surface
         *  (see AGE-2756). */}
        <DialogPrimitive.Root
          open={sheetOpen}
          onOpenChange={(open) => {
            if (!open) {
              closeSheet();
            }
          }}
        >
          <DialogPrimitive.Portal>
            <DialogPrimitive.Overlay className="bg-background fixed inset-0 z-50" />
            <DialogPrimitive.Content
              onInteractOutside={(e) => e.preventDefault()}
              className="bg-surface-primary fixed inset-2 z-50 flex flex-col overflow-hidden rounded-xl border shadow-sm focus:outline-none"
            >
              <div className="border-border flex items-start justify-between gap-4 border-b px-8 py-5">
                <div className="mx-auto flex w-full max-w-5xl items-start justify-between gap-4">
                  <div className="min-w-0">
                    <DialogPrimitive.Title asChild>
                      <Type variant="subheading">{sheetTitle}</Type>
                    </DialogPrimitive.Title>
                    <DialogPrimitive.Description asChild>
                      <Type small muted className="mt-1">
                        {sheetDescription}
                      </Type>
                    </DialogPrimitive.Description>
                  </div>
                  <Button variant="tertiary" onClick={closeSheet}>
                    <Button.LeftIcon>
                      <X className="h-4 w-4" />
                    </Button.LeftIcon>
                    <Button.Text>Close</Button.Text>
                  </Button>
                </div>
              </div>
              <div className="flex-1 overflow-y-auto px-8">
                <div className="mx-auto w-full max-w-5xl space-y-6 py-8">
                  {isChoosingPolicyKind ? (
                    <PolicyKindChoice onSelect={handleChoosePolicyKind} />
                  ) : formPolicyKind === "risk" ? (
                    <PolicySheetBody
                      key={editingPolicy?.id ?? "new-risk-policy"}
                      wizardStep={wizardStep}
                      setWizardStep={setWizardStep}
                      formName={formName}
                      setFormName={setFormName}
                      formEnabled={formEnabled}
                      setFormEnabled={setFormEnabled}
                      selectedCategories={selectedCategories}
                      setSelectedCategories={setSelectedCategories}
                      formPresidioThreshold={formPresidioThreshold}
                      setFormPresidioThreshold={setFormPresidioThreshold}
                      disabledRules={disabledRules}
                      setDisabledRules={setDisabledRules}
                      customRules={customRules}
                      selectedCustomRuleIds={selectedCustomRuleIds}
                      setSelectedCustomRuleIds={setSelectedCustomRuleIds}
                      scopeInclude={scopeInclude}
                      setScopeInclude={setScopeInclude}
                      scopeExempt={scopeExempt}
                      setScopeExempt={setScopeExempt}
                      scopeMode={scopeMode}
                      setScopeMode={setScopeMode}
                      selectedMessageTypes={selectedMessageTypes}
                      setSelectedMessageTypes={setSelectedMessageTypes}
                      formAction={formAction}
                      setFormAction={setFormAction}
                      formAutoName={formAutoName}
                      setFormAutoName={setFormAutoName}
                      formUserMessage={formUserMessage}
                      setFormUserMessage={setFormUserMessage}
                      formAudienceType={formAudienceType}
                      setFormAudienceType={setFormAudienceType}
                      selectedAudiencePrincipalUrns={
                        selectedAudiencePrincipalUrns
                      }
                      setSelectedAudiencePrincipalUrns={
                        setSelectedAudiencePrincipalUrns
                      }
                    />
                  ) : (
                    <PromptPolicySheetBody
                      key={editingPolicy?.id ?? "new-prompt-policy"}
                      wizardStep={wizardStep}
                      setWizardStep={setWizardStep}
                      isEditing={!!editingPolicy}
                      formName={formName}
                      setFormName={setFormName}
                      formPromptInstruction={formPromptInstruction}
                      setFormPromptInstruction={setFormPromptInstruction}
                      formAction={formAction}
                      setFormAction={setFormAction}
                      formAutoName={formAutoName}
                      setFormAutoName={setFormAutoName}
                      formEnabled={formEnabled}
                      setFormEnabled={setFormEnabled}
                      formModel={formModel}
                      setFormModel={setFormModel}
                      formTemperature={formTemperature}
                      setFormTemperature={setFormTemperature}
                      formFailOpen={formFailOpen}
                      setFormFailOpen={setFormFailOpen}
                      scopeInclude={scopeInclude}
                      setScopeInclude={setScopeInclude}
                      scopeExempt={scopeExempt}
                      setScopeExempt={setScopeExempt}
                      scopeMode={scopeMode}
                      setScopeMode={setScopeMode}
                      selectedMessageTypes={selectedMessageTypes}
                      setSelectedMessageTypes={setSelectedMessageTypes}
                    />
                  )}
                </div>
              </div>
              {!isChoosingPolicyKind && (
                <div className="border-border bg-background border-t px-8 py-4">
                  <div className="mx-auto flex w-full max-w-5xl flex-row items-center justify-between">
                    {isWizard ? (
                      <span className="text-muted-foreground text-xs">
                        Step {wizardStep + 1} of {wizardSteps.length} ·{" "}
                        {wizardSteps[wizardStep]?.title}
                      </span>
                    ) : (
                      <span />
                    )}
                    <div className="flex gap-2">
                      {showFooterBack && (
                        <Button variant="secondary" onClick={onFooterBack}>
                          <Button.LeftIcon>
                            <ArrowLeft className="h-4 w-4" />
                          </Button.LeftIcon>
                          <Button.Text>Back</Button.Text>
                        </Button>
                      )}
                      {showWizardContinue ? (
                        <Button
                          onClick={() => setWizardStep(wizardStep + 1)}
                          disabled={continueDisabled}
                        >
                          <Button.Text>Continue</Button.Text>
                        </Button>
                      ) : (
                        <Button onClick={handleSave} disabled={saveDisabled}>
                          {mutationPending && (
                            <Button.LeftIcon>
                              <Loader2 className="h-4 w-4 animate-spin" />
                            </Button.LeftIcon>
                          )}
                          <Button.Text>
                            {mutationPending
                              ? "Saving..."
                              : editingPolicy
                                ? "Update"
                                : "Create"}
                          </Button.Text>
                        </Button>
                      )}
                    </div>
                  </div>
                </div>
              )}
            </DialogPrimitive.Content>
          </DialogPrimitive.Portal>
        </DialogPrimitive.Root>

        {/* View Run Panel */}
        <Sheet
          open={!!runPanelPolicy}
          onOpenChange={(open) => {
            if (!open) setRunPanelPolicy(null);
          }}
        >
          <SheetContent side="right" className="sm:max-w-md">
            {runPanelPolicy && <RunPanel policy={runPanelPolicy} />}
          </SheetContent>
        </Sheet>
      </Page.Body>
    </Page>
  );
}

/* -------------------------------------------------------------------------- */
/*  PolicyKindChoice                                                          */
/* -------------------------------------------------------------------------- */

function PolicyKindChoice({
  onSelect,
}: {
  onSelect: (kind: PolicyKind) => void;
}) {
  return (
    <div className="space-y-3">
      <button
        type="button"
        onClick={() => onSelect("risk")}
        className="border-border hover:bg-muted/50 focus-visible:ring-ring flex w-full items-start gap-3 rounded-lg border p-4 text-left transition-colors focus-visible:ring-2 focus-visible:outline-none"
      >
        <div className="bg-muted mt-0.5 flex h-9 w-9 shrink-0 items-center justify-center rounded-md">
          <Shield className="text-muted-foreground h-4 w-4" />
        </div>
        <div className="min-w-0 flex-1">
          <Type className="font-medium">Standard</Type>
          <Type small muted className="mt-0.5">
            Catch secrets, PII, and destructive commands using built-in scanners
            and custom regex rules.
          </Type>
        </div>
        <ChevronRight className="text-muted-foreground mt-2.5 h-4 w-4 shrink-0" />
      </button>
      <button
        type="button"
        onClick={() => onSelect("prompt")}
        className="border-border hover:bg-muted/50 focus-visible:ring-ring flex w-full items-start gap-3 rounded-lg border p-4 text-left transition-colors focus-visible:ring-2 focus-visible:outline-none"
      >
        <div className="bg-muted mt-0.5 flex h-9 w-9 shrink-0 items-center justify-center rounded-md">
          <Sparkles className="text-muted-foreground h-4 w-4" />
        </div>
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <Type className="font-medium">Prompt-based</Type>
            <Badge variant="neutral" className="text-[10px]">
              <Badge.Text>New</Badge.Text>
            </Badge>
          </div>
          <Type small muted className="mt-0.5">
            Describe any behavior you want to detect in plain language. No
            scanner configuration needed.
          </Type>
        </div>
        <ChevronRight className="text-muted-foreground mt-2.5 h-4 w-4 shrink-0" />
      </button>
    </div>
  );
}

/* -------------------------------------------------------------------------- */
/*  PromptPolicySheetBody                                                     */
/* -------------------------------------------------------------------------- */

function PromptPolicySheetBody({
  wizardStep,
  setWizardStep,
  isEditing,
  formName,
  setFormName,
  formPromptInstruction,
  setFormPromptInstruction,
  formAction,
  setFormAction,
  formAutoName,
  setFormAutoName,
  formEnabled,
  setFormEnabled,
  formModel,
  setFormModel,
  formTemperature,
  setFormTemperature,
  formFailOpen,
  setFormFailOpen,
  scopeInclude,
  setScopeInclude,
  scopeExempt,
  setScopeExempt,
  scopeMode,
  setScopeMode,
  selectedMessageTypes,
  setSelectedMessageTypes,
}: {
  wizardStep: number;
  setWizardStep: (v: number) => void;
  isEditing: boolean;
  formName: string;
  setFormName: (v: string) => void;
  formPromptInstruction: string;
  setFormPromptInstruction: (v: string) => void;
  formAction: PolicyAction;
  setFormAction: (v: PolicyAction) => void;
  formAutoName: boolean;
  setFormAutoName: (v: boolean) => void;
  formEnabled: boolean;
  setFormEnabled: (v: boolean) => void;
  formModel: string;
  setFormModel: (v: string) => void;
  formTemperature: number;
  setFormTemperature: (v: number) => void;
  formFailOpen: boolean;
  setFormFailOpen: (v: boolean) => void;
  scopeInclude: string;
  setScopeInclude: (v: string) => void;
  scopeExempt: string;
  setScopeExempt: (v: string) => void;
  scopeMode: "messageTypes" | "cel";
  setScopeMode: (v: "messageTypes" | "cel") => void;
  selectedMessageTypes: Set<PolicyMessageType>;
  setSelectedMessageTypes: (v: Set<PolicyMessageType>) => void;
}) {
  const [selectedExampleName, setSelectedExampleName] = useState(
    () => promptTemplateNameForInstruction(formPromptInstruction) ?? "",
  );
  const [advancedOpen, setAdvancedOpen] = useState(false);

  const handlePromptChange = (value: string) => {
    setSelectedExampleName("");
    setFormPromptInstruction(value);
  };

  const prompt = formPromptInstruction.trim();
  const messageTypeScopeLabels =
    selectedMessageTypes.size === ALL_POLICY_MESSAGE_TYPES.length
      ? ["All session parts"]
      : ALL_POLICY_MESSAGE_TYPES.filter((t) =>
          selectedMessageTypes.has(t as PolicyMessageType),
        ).map((t) => POLICY_MESSAGE_TYPE_META[t as PolicyMessageType].label);
  const summaryScopes =
    scopeMode === "cel" ? ["CEL expression"] : messageTypeScopeLabels;

  // One-line view of the judge config shown on the collapsed Advanced card, so
  // authors can see the (sensible) defaults at a glance without expanding it.
  const judgeModelLabel =
    JUDGE_MODEL_OPTIONS.find((o) => o.value === formModel)?.label ??
    (formModel || JUDGE_MODEL_OPTIONS[0]?.label) ??
    "Default model";
  const judgeSummary = `${judgeModelLabel} · temp ${formTemperature.toFixed(1)} · ${formFailOpen ? "fail-open" : "fail-closed"}`;

  return (
    <WizardShell
      steps={PROMPT_WIZARD_STEPS}
      currentStep={wizardStep}
      setCurrentStep={setWizardStep}
    >
      {wizardStep === 0 && (
        <div className="space-y-6">
          <WizardStepHeading
            title="What should this policy catch?"
            description="Describe the behavior to detect in plain language; the LLM judge evaluates each in-scope message against it."
          />
          <PromptPolicyHowItWorks isEditing={isEditing} />
          <div className="space-y-2">
            <Label className="text-sm font-medium">Policy Prompt</Label>
            <TextArea
              value={formPromptInstruction}
              onChange={handlePromptChange}
              placeholder="Describe the tool-call behavior this policy should match..."
              rows={5}
            />
            {!isEditing && (
              <PromptExampleChips
                selectedExampleName={selectedExampleName}
                onSelect={(template) => {
                  setSelectedExampleName(template.name);
                  setFormPromptInstruction(template.prompt);
                  if (!formName.trim()) {
                    setFormName(template.name);
                  }
                }}
              />
            )}
          </div>
          <Collapsible
            open={advancedOpen}
            onOpenChange={setAdvancedOpen}
            className="border-border rounded-lg border"
          >
            <CollapsibleTrigger className="hover:bg-muted/40 flex w-full items-center gap-3 px-4 py-3 text-left transition-colors">
              <SlidersHorizontal className="text-muted-foreground h-4 w-4 shrink-0" />
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2">
                  <span className="text-sm font-medium">
                    Advanced judge settings
                  </span>
                  <Badge variant="neutral" className="text-[10px]">
                    <Badge.Text>Optional</Badge.Text>
                  </Badge>
                </div>
                <div className="text-muted-foreground truncate text-xs">
                  {advancedOpen
                    ? "Judge model, temperature, and failure behavior"
                    : judgeSummary}
                </div>
              </div>
              <ChevronRight
                className={cn(
                  "text-muted-foreground h-4 w-4 shrink-0 transition-transform",
                  advancedOpen && "rotate-90",
                )}
              />
            </CollapsibleTrigger>
            <CollapsibleContent className="border-border border-t px-4 py-4">
              <JudgeConfigSection
                formModel={formModel}
                setFormModel={setFormModel}
                formTemperature={formTemperature}
                setFormTemperature={setFormTemperature}
                formFailOpen={formFailOpen}
                setFormFailOpen={setFormFailOpen}
              />
            </CollapsibleContent>
          </Collapsible>
        </div>
      )}

      {wizardStep === 1 && (
        <div className="space-y-6">
          <WizardStepHeading
            title="Where should it evaluate?"
            description="Narrow the scope to control cost — a prompt policy runs the LLM judge on each in-scope message."
          />
          {/* Scope is a mutex: message-type cards (coarse) XOR a CEL include
              predicate (fine). The segmented control conveys that. */}
          <div className="space-y-3">
            <div className="border-border inline-flex rounded-md border p-0.5">
              {(
                [
                  { key: "messageTypes", label: "Message types" },
                  { key: "cel", label: "CEL expression" },
                ] as const
              ).map((opt) => (
                <button
                  key={opt.key}
                  type="button"
                  onClick={() => setScopeMode(opt.key)}
                  className={cn(
                    "rounded px-3 py-1 text-xs font-medium transition-colors",
                    scopeMode === opt.key
                      ? "bg-foreground text-background"
                      : "text-muted-foreground hover:text-foreground",
                  )}
                >
                  {opt.label}
                </button>
              ))}
            </div>
            <p className="text-muted-foreground text-xs">
              {scopeMode === "messageTypes"
                ? "Run the judge on whole session parts. Switch to a CEL expression to match on tool or content attributes instead."
                : "Run the judge only on messages matching the expression below — this replaces the message-type selection."}
            </p>
          </div>

          {scopeMode === "messageTypes" ? (
            <>
              <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                {ALL_POLICY_MESSAGE_TYPES.map((type) => (
                  <ScopeCard
                    key={type}
                    type={type as PolicyMessageType}
                    checked={selectedMessageTypes.has(
                      type as PolicyMessageType,
                    )}
                    onToggle={(checked) => {
                      const updated = new Set(selectedMessageTypes);
                      if (checked) {
                        updated.add(type as PolicyMessageType);
                      } else {
                        updated.delete(type as PolicyMessageType);
                      }
                      setSelectedMessageTypes(updated);
                    }}
                  />
                ))}
              </div>
              {selectedMessageTypes.size === 0 && (
                <p className="text-destructive text-xs">
                  Select at least one session part.
                </p>
              )}
            </>
          ) : (
            <div className="space-y-2">
              <Label className="text-sm font-medium">
                Evaluate messages matching
              </Label>
              <p className="text-muted-foreground text-xs">
                The judge runs on a message only when this expression is true.
              </p>
              <CelExpressionField
                value={scopeInclude}
                onChange={setScopeInclude}
                examples={SCOPE_INCLUDE_CEL_EXAMPLES}
              />
            </div>
          )}

          {/* Exemptions — always available and additive (not part of the
              scope mutex). A match here skips the whole policy. */}
          <div className="border-border space-y-4 border-t pt-6">
            <div>
              <Label className="text-sm font-medium">Exemptions</Label>
              <p className="text-muted-foreground text-xs">
                Skip the whole policy for any message matching this expression —
                an allowlist, regardless of the scope above.
              </p>
            </div>
            <CelExpressionField
              value={scopeExempt}
              onChange={setScopeExempt}
              examples={SCOPE_EXEMPT_CEL_EXAMPLES}
            />
          </div>
        </div>
      )}

      {wizardStep === 2 && (
        <div className="space-y-6">
          <WizardStepHeading
            title="What happens on a match?"
            description="Choose how the policy responds when the judge flags a message."
          />
          <ActionPicker formAction={formAction} setFormAction={setFormAction} />
        </div>
      )}

      {wizardStep === 3 && (
        <div className="space-y-6">
          <WizardStepHeading
            title="Name & enable"
            description="Review the policy, then create it."
          />
          <div className="space-y-2">
            <div className="flex items-center justify-between">
              <Label className="text-sm font-medium">Policy Name</Label>
              <div className="flex items-center gap-2">
                <span className="text-muted-foreground text-xs">Auto</span>
                <Switch
                  checked={formAutoName}
                  onCheckedChange={setFormAutoName}
                />
              </div>
            </div>
            {formAutoName ? (
              <p className="text-muted-foreground text-xs">
                Name will be generated from the policy prompt.
              </p>
            ) : (
              <Input
                value={formName}
                onChange={setFormName}
                placeholder="e.g. No Production Deletes"
              />
            )}
          </div>
          <div className="space-y-2">
            <Label className="text-sm font-medium">Summary</Label>
            <div className="border-border divide-border divide-y rounded-lg border">
              <SummaryRow
                label="Guardrail"
                chips={[
                  prompt
                    ? `${prompt.slice(0, 48)}${prompt.length > 48 ? "…" : ""}`
                    : "None",
                ]}
              />
              <SummaryRow label="Scope" chips={summaryScopes} />
              <SummaryRow
                label="Action"
                chips={[formAction === "block" ? "Block" : "Flag"]}
              />
            </div>
          </div>
          <div className="flex items-center justify-between">
            <div>
              <Label className="text-sm font-medium">Enabled</Label>
              <p className="text-muted-foreground text-xs">
                Enable this policy to enforce the prompt.
              </p>
            </div>
            <Switch checked={formEnabled} onCheckedChange={setFormEnabled} />
          </div>
        </div>
      )}
    </WizardShell>
  );
}

/* -------------------------------------------------------------------------- */
/*  JudgeConfigSection                                                        */
/* -------------------------------------------------------------------------- */

// DEFAULT_JUDGE_TEMPERATURE mirrors riskjudge.defaultJudgeTemperature: the
// benchmark ran at 0 (deterministic verdicts), which is the effective default.
const DEFAULT_JUDGE_TEMPERATURE = 0;
const TEMPERATURE_MIN = 0;
const TEMPERATURE_MAX = 1;
const TEMPERATURE_STEP = 0.1;
// Discrete stops rendered on the slider track: 0.0, 0.1, … 1.0.
const TEMPERATURE_TICKS = Array.from(
  { length: 11 },
  (_, i) => Math.round(i * TEMPERATURE_STEP * 10) / 10,
);

// Presidio detection-sensitivity threshold: the minimum confidence score a
// Presidio PII match must clear to be flagged. Applies to all Presidio rules in
// a standard risk policy.
const PRESIDIO_THRESHOLD_MIN = 0;
const PRESIDIO_THRESHOLD_MAX = 1;
const PRESIDIO_THRESHOLD_STEP = 0.05;
const PRESIDIO_THRESHOLD_TICKS = [0, 0.25, 0.5, 0.75, 1];
const DEFAULT_PRESIDIO_THRESHOLD = 0.5;

// JUDGE_MODEL_OPTIONS lists the models a prompt policy may run its LLM judge on.
// The recommended option uses the empty value, which follows the server's
// default judge model. Its label names that model and must stay in sync with
// riskjudge.defaultJudgeModel. See server/cmd/riskjudgebench for the benchmark
// behind these picks.
const JUDGE_MODEL_OPTIONS: {
  value: string;
  label: string;
  description: string;
  recommended?: boolean;
}[] = [
  {
    value: "",
    label: "Gemini 3.1 Flash Lite",
    description:
      "Fast, low-cost, high-recall classifier. Best fit for most policies.",
    recommended: true,
  },
  {
    value: "google/gemini-2.5-flash",
    label: "Gemini 2.5 Flash",
    description: "Lowest latency. Good for very high-volume policies.",
  },
  {
    value: "anthropic/claude-sonnet-4.6",
    label: "Claude Sonnet 4.6",
    description:
      "Strong quality and the highest ceiling, at higher latency and cost.",
  },
  {
    value: "anthropic/claude-haiku-4.5",
    label: "Claude Haiku 4.5",
    description: "Balanced Anthropic option.",
  },
];

function JudgeConfigSection({
  formModel,
  setFormModel,
  formTemperature,
  setFormTemperature,
  formFailOpen,
  setFormFailOpen,
}: {
  formModel: string;
  setFormModel: (v: string) => void;
  formTemperature: number;
  setFormTemperature: (v: number) => void;
  formFailOpen: boolean;
  setFormFailOpen: (v: boolean) => void;
}) {
  return (
    <div className="space-y-4">
      <JudgeModelPicker formModel={formModel} setFormModel={setFormModel} />

      <div className="border-border space-y-4 rounded-lg border p-4">
        <div className="space-y-2">
          <div className="flex items-center gap-2">
            <Label className="text-sm font-medium">Temperature</Label>
            <span className="text-muted-foreground text-xs tabular-nums">
              {formTemperature.toFixed(1)}
              {formTemperature === DEFAULT_JUDGE_TEMPERATURE
                ? " · default"
                : ""}
            </span>
          </div>
          <div className="flex items-center gap-3">
            <span className="text-foreground text-xs tabular-nums">0</span>
            <div className="flex-1">
              <Slider
                value={formTemperature}
                onChange={(v) => setFormTemperature(Math.round(v * 10) / 10)}
                min={TEMPERATURE_MIN}
                max={TEMPERATURE_MAX}
                step={TEMPERATURE_STEP}
                ticks={TEMPERATURE_TICKS}
              />
            </div>
            <span className="text-foreground text-xs tabular-nums">1</span>
          </div>
          <p className="text-muted-foreground text-xs">
            Lower is more consistent and repeatable (0 recommended). Higher adds
            variation, which can surface borderline or unusual violations a
            rigid read might miss.
          </p>
        </div>

        <div className="border-border flex items-start justify-between gap-4 border-t pt-4">
          <div className="space-y-2 pr-2">
            <Label className="text-sm font-medium">Fail open</Label>
            <p className="text-muted-foreground text-xs">
              When the judge errors or times out, allow the message instead of
              blocking it.
            </p>
          </div>
          <Switch checked={formFailOpen} onCheckedChange={setFormFailOpen} />
        </div>
      </div>
    </div>
  );
}

function JudgeModelPicker({
  formModel,
  setFormModel,
}: {
  formModel: string;
  setFormModel: (v: string) => void;
}) {
  // A model persisted via the API but not in the curated list still needs to
  // round-trip, so surface it as a selectable option.
  const knownValues = JUDGE_MODEL_OPTIONS.map((o) => o.value);
  const options =
    formModel && !knownValues.includes(formModel)
      ? [
          ...JUDGE_MODEL_OPTIONS,
          {
            value: formModel,
            label: formModel,
            description: "Custom model configured via the API.",
          },
        ]
      : JUDGE_MODEL_OPTIONS;

  return (
    <div className="space-y-2">
      <Label className="text-sm font-medium">Model</Label>
      <RadioGroup value={formModel} onValueChange={setFormModel}>
        <div className="border-border divide-border divide-y rounded-lg border">
          {options.map((opt) => (
            <label
              key={opt.value || "recommended"}
              htmlFor={`judge-model-${opt.value || "recommended"}`}
              className="hover:bg-muted/50 flex cursor-pointer items-start gap-3 p-3"
            >
              <RadioGroupItem
                value={opt.value}
                id={`judge-model-${opt.value || "recommended"}`}
                className="mt-0.5"
              />
              <div className="flex-1">
                <div className="flex items-center gap-2">
                  <span className="text-sm font-medium">{opt.label}</span>
                  {opt.recommended && (
                    <Badge variant="neutral" className="text-[10px]">
                      <Badge.Text>Recommended</Badge.Text>
                    </Badge>
                  )}
                </div>
                <div className="text-muted-foreground mt-1 text-xs">
                  {opt.description}
                </div>
              </div>
            </label>
          ))}
        </div>
      </RadioGroup>
    </div>
  );
}

function PromptExampleChips({
  selectedExampleName,
  onSelect,
}: {
  selectedExampleName: string;
  onSelect: (template: (typeof PROMPT_POLICY_TEMPLATES)[number]) => void;
}) {
  return (
    <div className="flex flex-wrap items-center gap-1.5">
      <span className="text-muted-foreground text-xs">Try:</span>
      {PROMPT_POLICY_TEMPLATES.map((template) => {
        const active = selectedExampleName === template.name;
        return (
          <SimpleTooltip key={template.name} tooltip={template.prompt}>
            <button
              type="button"
              aria-pressed={active}
              onClick={() => onSelect(template)}
              className={cn(
                "rounded-full border px-2.5 py-1 text-xs transition-colors",
                active
                  ? "border-foreground/30 bg-muted text-foreground"
                  : "border-border text-muted-foreground hover:bg-muted/50 hover:text-foreground",
              )}
            >
              {template.name}
            </button>
          </SimpleTooltip>
        );
      })}
    </div>
  );
}

function PromptPolicyHowItWorks({ isEditing }: { isEditing: boolean }) {
  const [open, setOpen] = useState(!isEditing);

  return (
    <Collapsible
      open={open}
      onOpenChange={setOpen}
      className="border-border rounded-lg border"
    >
      <CollapsibleTrigger className="hover:bg-muted/40 flex w-full items-start gap-3 px-4 py-3 text-left transition-colors">
        <Info className="text-muted-foreground mt-0.5 h-4 w-4 shrink-0" />
        <div className="min-w-0 flex-1">
          <div className="text-sm font-medium">How this works</div>
          <div className="text-muted-foreground text-xs">
            An LLM judge reads each in-scope message and flags it when it
            matches your prompt.
          </div>
        </div>
        <ChevronRight
          className={cn(
            "text-muted-foreground mt-0.5 h-4 w-4 shrink-0 transition-transform",
            open && "rotate-90",
          )}
        />
      </CollapsibleTrigger>
      <CollapsibleContent className="border-border border-t px-4 py-3">
        <p className="text-muted-foreground text-sm">
          Prompt-based policies call an LLM judge, so every in-scope message
          adds latency and cost versus the deterministic detection rules. The
          judge sees one message at a time — a tool call and its inputs, or
          message content — never the whole conversation. Narrow <b>Scope</b> to
          control which messages it runs on, and add <b>Exemptions</b> to skip
          trusted ones and keep cost down.
        </p>
      </CollapsibleContent>
    </Collapsible>
  );
}

/* -------------------------------------------------------------------------- */
/*  PolicySheetBody                                                           */
/* -------------------------------------------------------------------------- */

function PolicySheetBody({
  wizardStep,
  setWizardStep,
  formName,
  setFormName,
  formEnabled,
  setFormEnabled,
  selectedCategories,
  setSelectedCategories,
  formPresidioThreshold,
  setFormPresidioThreshold,
  disabledRules,
  setDisabledRules,
  customRules,
  selectedCustomRuleIds,
  setSelectedCustomRuleIds,
  scopeInclude,
  setScopeInclude,
  scopeExempt,
  setScopeExempt,
  scopeMode,
  setScopeMode,
  selectedMessageTypes,
  setSelectedMessageTypes,
  formAction,
  setFormAction,
  formAutoName,
  setFormAutoName,
  formUserMessage,
  setFormUserMessage,
  formAudienceType,
  setFormAudienceType,
  selectedAudiencePrincipalUrns,
  setSelectedAudiencePrincipalUrns,
}: {
  wizardStep: number;
  setWizardStep: (v: number) => void;
  formName: string;
  setFormName: (v: string) => void;
  formEnabled: boolean;
  setFormEnabled: (v: boolean) => void;
  selectedCategories: Set<RuleCategory>;
  setSelectedCategories: (v: Set<RuleCategory>) => void;
  formPresidioThreshold: number;
  setFormPresidioThreshold: (v: number) => void;
  disabledRules: Set<string>;
  setDisabledRules: (v: Set<string>) => void;
  customRules: ReturnType<typeof useDetectionRulesStore>["customRules"];
  selectedCustomRuleIds: Set<string>;
  setSelectedCustomRuleIds: (v: Set<string>) => void;
  scopeInclude: string;
  setScopeInclude: (v: string) => void;
  scopeExempt: string;
  setScopeExempt: (v: string) => void;
  scopeMode: "messageTypes" | "cel";
  setScopeMode: (v: "messageTypes" | "cel") => void;
  selectedMessageTypes: Set<PolicyMessageType>;
  setSelectedMessageTypes: (v: Set<PolicyMessageType>) => void;
  formAction: PolicyAction;
  setFormAction: (v: PolicyAction) => void;
  formAutoName: boolean;
  setFormAutoName: (v: boolean) => void;
  formUserMessage: string;
  setFormUserMessage: (v: string) => void;
  formAudienceType: PolicyAudienceType;
  setFormAudienceType: (v: PolicyAudienceType) => void;
  selectedAudiencePrincipalUrns: Set<string>;
  setSelectedAudiencePrincipalUrns: (v: Set<string>) => void;
}) {
  // The org's custom rules collapse into their own section; the Customize sheet
  // opens for one detector category at a time.
  const [detectionExpanded, setDetectionExpanded] = useState(true);
  const [customizeCategory, setCustomizeCategory] =
    useState<RuleCategory | null>(null);
  const selectedBuiltinCount = ALL_CATEGORIES.filter((c) =>
    selectedCategories.has(c),
  ).length;

  // Toggle a whole built-in detector category on/off (clears any per-rule
  // disables for it). Flag-only categories force the policy action to flag.
  const toggleCategory = (cat: RuleCategory, checked: boolean) => {
    const rules = DETECTION_RULES[cat].filter((r) => !r.hidden);
    const nextCats = new Set(selectedCategories);
    const nextDisabled = new Set(disabledRules);
    if (checked) {
      nextCats.add(cat);
    } else {
      nextCats.delete(cat);
    }
    for (const rule of rules) nextDisabled.delete(rule.id);
    setSelectedCategories(nextCats);
    setDisabledRules(nextDisabled);
    if (checked && FLAG_ONLY_CATEGORIES.has(cat) && formAction === "block") {
      setFormAction("flag");
    }
  };
  const flagOnlySelected = [...FLAG_ONLY_CATEGORIES].some((c) =>
    selectedCategories.has(c),
  );
  // The detection-sensitivity slider only applies when a Presidio detector is on.
  const presidioActive = PRESIDIO_CATEGORIES.some((c) =>
    selectedCategories.has(c),
  );

  // Custom rules attach as detectors only; a match records a finding. Message
  // exemptions are expressed via the policy's scope_exempt, not by rule id.
  const toggleDetector = (ruleId: string, checked: boolean) => {
    const next = new Set(selectedCustomRuleIds);
    if (checked) {
      next.add(ruleId);
    } else {
      next.delete(ruleId);
    }
    setSelectedCustomRuleIds(next);
  };

  // Review-step summary chips.
  const summaryDetectors = ALL_CATEGORIES.filter((c) =>
    selectedCategories.has(c),
  ).map((c) => RULE_CATEGORY_META[c].label);
  const messageTypeScopeLabels =
    selectedMessageTypes.size === ALL_POLICY_MESSAGE_TYPES.length
      ? ["All session parts"]
      : ALL_POLICY_MESSAGE_TYPES.filter((t) =>
          selectedMessageTypes.has(t as PolicyMessageType),
        ).map((t) => POLICY_MESSAGE_TYPE_META[t as PolicyMessageType].label);
  const summaryScopes =
    scopeMode === "cel" ? ["CEL expression"] : messageTypeScopeLabels;

  // Render by step id so the standard layout stays correct after the
  // "sensitivity" step was inserted between Detect and Scope.
  const currentStepId = POLICY_WIZARD_STEPS[wizardStep]?.id;

  return (
    <>
      <WizardShell
        steps={POLICY_WIZARD_STEPS}
        currentStep={wizardStep}
        setCurrentStep={setWizardStep}
      >
        {currentStepId === "detect" && (
          <div className="space-y-6">
            <WizardStepHeading
              title="What should this policy detect?"
              description="Turn on detector categories and attach your organization's custom rules."
            />

            {/* Built-in rules */}
            <div className="space-y-3">
              <div className="flex items-center justify-between">
                <Label className="text-sm font-medium">Built-in rules</Label>
                <span className="text-muted-foreground text-xs">
                  {selectedBuiltinCount} on
                </span>
              </div>
              <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                {ALL_CATEGORIES.map((cat) => (
                  <DetectorCard
                    key={cat}
                    category={cat}
                    selected={selectedCategories.has(cat)}
                    disabledRules={disabledRules}
                    onToggle={(checked) => toggleCategory(cat, checked)}
                    onCustomize={() => setCustomizeCategory(cat)}
                  />
                ))}
              </div>
            </div>

            {customRules.length > 0 && (
              <RuleSelectList
                title="Custom Rules"
                description={
                  <>
                    Attach your organization's custom rules as{" "}
                    <span className="text-foreground font-medium">
                      detectors
                    </span>{" "}
                    — a match records a finding.
                  </>
                }
                idPrefix="detector"
                customRules={customRules}
                selectedRuleIds={selectedCustomRuleIds}
                onToggleRule={toggleDetector}
                expanded={detectionExpanded}
                onToggle={() => setDetectionExpanded((v) => !v)}
              />
            )}
          </div>
        )}

        {currentStepId === "sensitivity" && (
          <div className="space-y-6">
            <WizardStepHeading
              title="How sensitive should detection be?"
              description="Tune the confidence threshold a match must clear before it's flagged."
            />
            {presidioActive ? (
              <div className="border-border space-y-4 rounded-lg border p-4">
                <div className="space-y-2">
                  <div className="flex items-center gap-2">
                    <Label className="text-sm font-medium">
                      Detection sensitivity
                    </Label>
                    <span className="text-muted-foreground text-xs tabular-nums">
                      {formPresidioThreshold.toFixed(2)}
                      {formPresidioThreshold === DEFAULT_PRESIDIO_THRESHOLD
                        ? " · default"
                        : ""}
                    </span>
                  </div>
                  <div className="flex items-center gap-3">
                    <span className="text-foreground text-xs tabular-nums">
                      0
                    </span>
                    <div className="flex-1">
                      <Slider
                        value={formPresidioThreshold}
                        onChange={(v) =>
                          setFormPresidioThreshold(Math.round(v * 20) / 20)
                        }
                        min={PRESIDIO_THRESHOLD_MIN}
                        max={PRESIDIO_THRESHOLD_MAX}
                        step={PRESIDIO_THRESHOLD_STEP}
                        ticks={PRESIDIO_THRESHOLD_TICKS}
                      />
                    </div>
                    <span className="text-foreground text-xs tabular-nums">
                      1
                    </span>
                  </div>
                  <p className="text-muted-foreground text-xs">
                    Minimum confidence a match must clear to be flagged. Higher
                    = fewer false positives but may miss borderline matches.
                    Applies to all detection rules in this policy.
                  </p>
                </div>
              </div>
            ) : (
              <p className="text-muted-foreground text-sm">
                Sensitivity applies to confidence-scored detectors. Select a PII
                detector to adjust it.
              </p>
            )}
          </div>
        )}

        {currentStepId === "scope" && (
          <div className="space-y-6">
            <WizardStepHeading
              title="Where should it evaluate?"
              description="Apply everywhere, or narrow the scope to reduce noise and cost."
            />
            {/* Scope is a mutex: message-type cards (coarse) XOR a CEL include
                predicate (fine). The segmented control conveys that. */}
            <div className="space-y-3">
              <div className="border-border inline-flex rounded-md border p-0.5">
                {(
                  [
                    { key: "messageTypes", label: "Message types" },
                    { key: "cel", label: "CEL expression" },
                  ] as const
                ).map((opt) => (
                  <button
                    key={opt.key}
                    type="button"
                    onClick={() => setScopeMode(opt.key)}
                    className={cn(
                      "rounded px-3 py-1 text-xs font-medium transition-colors",
                      scopeMode === opt.key
                        ? "bg-foreground text-background"
                        : "text-muted-foreground hover:text-foreground",
                    )}
                  >
                    {opt.label}
                  </button>
                ))}
              </div>
              <p className="text-muted-foreground text-xs">
                {scopeMode === "messageTypes"
                  ? "Apply to whole session parts. Switch to a CEL expression to match on tool or content attributes instead."
                  : "Apply only to messages matching the expression below — this replaces the message-type selection."}
              </p>
            </div>

            {scopeMode === "messageTypes" ? (
              <>
                <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                  {ALL_POLICY_MESSAGE_TYPES.map((type) => (
                    <ScopeCard
                      key={type}
                      type={type as PolicyMessageType}
                      checked={selectedMessageTypes.has(
                        type as PolicyMessageType,
                      )}
                      onToggle={(checked) => {
                        const updated = new Set(selectedMessageTypes);
                        if (checked) {
                          updated.add(type as PolicyMessageType);
                        } else {
                          updated.delete(type as PolicyMessageType);
                        }
                        setSelectedMessageTypes(updated);
                      }}
                    />
                  ))}
                </div>
                {selectedMessageTypes.size === 0 && (
                  <p className="text-destructive text-xs">
                    Select at least one session part.
                  </p>
                )}
              </>
            ) : (
              <div className="space-y-2">
                <Label className="text-sm font-medium">
                  Evaluate messages matching
                </Label>
                <p className="text-muted-foreground text-xs">
                  The policy evaluates a message only when this expression is
                  true.
                </p>
                <CelExpressionField
                  value={scopeInclude}
                  onChange={setScopeInclude}
                  examples={SCOPE_INCLUDE_CEL_EXAMPLES}
                />
              </div>
            )}

            {/* Exemptions — always available and additive (not part of the
                scope mutex). A match here skips the whole policy. */}
            <div className="border-border space-y-4 border-t pt-6">
              <div>
                <Label className="text-sm font-medium">Exemptions</Label>
                <p className="text-muted-foreground text-xs">
                  Skip the whole policy for any message matching this expression
                  — an allowlist, regardless of the scope above.
                </p>
              </div>
              <CelExpressionField
                value={scopeExempt}
                onChange={setScopeExempt}
                examples={SCOPE_EXEMPT_CEL_EXAMPLES}
              />
            </div>
          </div>
        )}

        {currentStepId === "action" && (
          <div className="space-y-6">
            <WizardStepHeading
              title="What happens on a match?"
              description="Choose how the policy responds when its detection rules fire."
            />
            <ActionPicker
              formAction={formAction}
              setFormAction={setFormAction}
              flagOnlySelected={flagOnlySelected}
            />

            {/* Who the policy applies to (audience). */}
            <PolicyAudiencePicker
              formAudienceType={formAudienceType}
              setFormAudienceType={setFormAudienceType}
              selectedAudiencePrincipalUrns={selectedAudiencePrincipalUrns}
              setSelectedAudiencePrincipalUrns={
                setSelectedAudiencePrincipalUrns
              }
            />

            {/* Custom message — only relevant for block-action policies that
          surface a user-facing reason at deny time. Flag-action policies
          record findings silently, so no message is needed. */}
            {formAction === "block" && (
              <div className="space-y-2">
                <Label className="text-sm font-medium">Custom Message</Label>
                <p className="text-muted-foreground text-xs">
                  Shown to the user when this policy blocks a tool call or
                  prompt. Leave blank to use the default message.
                </p>
                <TextArea
                  value={formUserMessage}
                  onChange={setFormUserMessage}
                  placeholder="e.g. This action was blocked by your organization's security policy. Contact your admin for help."
                  rows={3}
                />
              </div>
            )}
          </div>
        )}

        {currentStepId === "review" && (
          <div className="space-y-6">
            <WizardStepHeading
              title="Name & enable"
              description="Review the policy, then create it."
            />

            {/* Policy Name */}
            <div className="space-y-2">
              <div className="flex items-center justify-between">
                <Label className="text-sm font-medium">Policy Name</Label>
                <div className="flex items-center gap-2">
                  <span className="text-muted-foreground text-xs">Auto</span>
                  <Switch
                    checked={formAutoName}
                    onCheckedChange={setFormAutoName}
                  />
                </div>
              </div>
              {formAutoName ? (
                <p className="text-muted-foreground text-xs">
                  Name will be generated automatically based on detection rules
                  and action.
                </p>
              ) : (
                <Input
                  value={formName}
                  onChange={(value) => setFormName(value)}
                  placeholder="e.g. Secret Detection"
                />
              )}
            </div>

            {/* Summary */}
            <div className="space-y-2">
              <Label className="text-sm font-medium">Summary</Label>
              <div className="border-border divide-border divide-y rounded-lg border">
                <SummaryRow
                  label="Detectors"
                  chips={
                    summaryDetectors.length > 0 ? summaryDetectors : ["None"]
                  }
                />
                <SummaryRow
                  label="Custom rules"
                  chips={[
                    selectedCustomRuleIds.size > 0
                      ? `${selectedCustomRuleIds.size} attached`
                      : "None",
                  ]}
                />
                {scopeExempt.trim() !== "" && (
                  <SummaryRow label="Exemptions" chips={["CEL expression"]} />
                )}
                <SummaryRow label="Scope" chips={summaryScopes} />
                <SummaryRow
                  label="Action"
                  chips={[formAction === "block" ? "Block" : "Flag"]}
                />
              </div>
            </div>

            {/* Enabled toggle */}
            <div className="flex items-center justify-between">
              <div>
                <Label className="text-sm font-medium">Enabled</Label>
                <p className="text-muted-foreground text-xs">
                  Enable this policy to begin scanning messages.
                </p>
              </div>
              <Switch checked={formEnabled} onCheckedChange={setFormEnabled} />
            </div>
          </div>
        )}
      </WizardShell>
      {customizeCategory && (
        <CustomizeRulesSheet
          category={customizeCategory}
          selectedCategories={selectedCategories}
          setSelectedCategories={setSelectedCategories}
          disabledRules={disabledRules}
          setDisabledRules={setDisabledRules}
          onClose={() => setCustomizeCategory(null)}
        />
      )}
    </>
  );
}

function PolicyAudiencePicker({
  formAudienceType,
  setFormAudienceType,
  selectedAudiencePrincipalUrns,
  setSelectedAudiencePrincipalUrns,
}: {
  formAudienceType: PolicyAudienceType;
  setFormAudienceType: (v: PolicyAudienceType) => void;
  selectedAudiencePrincipalUrns: Set<string>;
  setSelectedAudiencePrincipalUrns: (v: Set<string>) => void;
}) {
  const { data: rolesData } = useRoles();
  const { data: membersData } = useMembers();
  const roles = useMemo(
    () => [...(rolesData?.roles ?? [])].sort(compareRolesByName),
    [rolesData?.roles],
  );
  const members = useMemo(
    () => [...(membersData?.members ?? [])].sort(compareMembersByName),
    [membersData?.members],
  );
  const [audienceChoice, setAudienceChoice] = useState<PolicyAudienceChoice>(
    () =>
      policyAudienceChoiceForSelection(
        formAudienceType,
        selectedAudiencePrincipalUrns,
      ),
  );
  const [userSearch, setUserSearch] = useState("");

  const togglePrincipal = (principalUrn: string, checked: boolean) => {
    const next = new Set(selectedAudiencePrincipalUrns);
    if (checked) {
      next.add(principalUrn);
    } else {
      next.delete(principalUrn);
    }
    setSelectedAudiencePrincipalUrns(next);
  };

  const selectAudienceChoice = (choice: PolicyAudienceChoice) => {
    setAudienceChoice(choice);
    setFormAudienceType(choice === "everyone" ? "everyone" : "targeted");
    setSelectedAudiencePrincipalUrns(
      filterAudiencePrincipalsForChoice(selectedAudiencePrincipalUrns, choice),
    );
    if (choice !== "users") {
      setUserSearch("");
    }
  };

  return (
    <div className="space-y-3">
      <div className="space-y-1">
        <Label className="text-sm font-medium">Audience</Label>
        <p className="text-muted-foreground text-xs">
          Choose which users this policy evaluates.
        </p>
      </div>
      <RadioGroup
        value={audienceChoice}
        onValueChange={(value) =>
          selectAudienceChoice(value as PolicyAudienceChoice)
        }
      >
        <div className="border-border divide-border divide-y rounded-lg border">
          <PolicyAudienceChoiceRow
            id="policy-audience-everyone"
            value="everyone"
            title="Everyone"
            description="Evaluate this policy for every user in the organization."
          />
          <PolicyAudienceChoiceRow
            id="policy-audience-users"
            value="users"
            title="Specific users"
            description="Search and select individual organization members."
          />
          <PolicyAudienceChoiceRow
            id="policy-audience-roles"
            value="roles"
            title="Specific roles"
            description="Evaluate this policy for every member of selected roles."
          />
        </div>
      </RadioGroup>

      {audienceChoice === "users" && (
        <SpecificUsersAudienceSection
          members={members}
          userSearch={userSearch}
          setUserSearch={setUserSearch}
          selectedAudiencePrincipalUrns={selectedAudiencePrincipalUrns}
          onTogglePrincipal={togglePrincipal}
        />
      )}

      {audienceChoice === "roles" && (
        <div className="border-border rounded-lg border">
          <AudiencePrincipalSection title="Roles">
            {roles.length === 0 ? (
              <p className="text-muted-foreground px-4 py-3 text-sm">
                No roles available.
              </p>
            ) : (
              roles.map((role) => {
                const principalUrn = role.principalUrn;
                return (
                  <AudiencePrincipalRow
                    key={principalUrn}
                    id={`audience-${principalUrn}`}
                    checked={selectedAudiencePrincipalUrns.has(principalUrn)}
                    title={role.name}
                    subtitle={`${role.memberCount} members`}
                    onCheckedChange={(checked) =>
                      togglePrincipal(principalUrn, checked)
                    }
                  />
                );
              })
            )}
          </AudiencePrincipalSection>
          {selectedAudiencePrincipalUrns.size === 0 && (
            <p className="text-muted-foreground border-border border-t px-4 py-3 text-xs">
              Select at least one role to save a targeted policy.
            </p>
          )}
        </div>
      )}
    </div>
  );
}

function SpecificUsersAudienceSection({
  members,
  userSearch,
  setUserSearch,
  selectedAudiencePrincipalUrns,
  onTogglePrincipal,
}: {
  members: AccessMember[];
  userSearch: string;
  setUserSearch: (value: string) => void;
  selectedAudiencePrincipalUrns: Set<string>;
  onTogglePrincipal: (principalUrn: string, checked: boolean) => void;
}) {
  const memberByPrincipalUrn = useMemo(
    () => new Map(members.map((member) => [member.principalUrn, member])),
    [members],
  );
  const selectedUserPrincipalUrns = useMemo(
    () =>
      [...selectedAudiencePrincipalUrns]
        .filter((principalUrn) => principalUrn.startsWith("user:"))
        .sort((a, b) => {
          const aMember = memberByPrincipalUrn.get(a);
          const bMember = memberByPrincipalUrn.get(b);
          const aLabel = aMember ? memberDisplayName(aMember) : a;
          const bLabel = bMember ? memberDisplayName(bMember) : b;
          return aLabel.localeCompare(bLabel);
        }),
    [memberByPrincipalUrn, selectedAudiencePrincipalUrns],
  );
  const selectedUserOptions = selectedUserPrincipalUrns.map((principalUrn) => {
    const member = memberByPrincipalUrn.get(principalUrn);
    return {
      member,
      principalUrn,
      title: member ? memberDisplayName(member) : principalUrn,
      subtitle: member?.email ?? "Unknown user",
    };
  });
  const matchingMembers = useMemo(
    () => members.filter((member) => memberMatchesSearch(member, userSearch)),
    [members, userSearch],
  );
  const unselectedMatchingMembers = matchingMembers.filter(
    (member) => !selectedAudiencePrincipalUrns.has(member.principalUrn),
  );
  const visibleSearchResults = unselectedMatchingMembers.slice(
    0,
    USER_SEARCH_RESULT_LIMIT,
  );
  const hiddenResultCount = Math.max(
    unselectedMatchingMembers.length - visibleSearchResults.length,
    0,
  );
  const hasSearch = userSearch.trim().length > 0;

  return (
    <div className="border-border rounded-lg border">
      <div className="space-y-4 p-4">
        <SearchBar
          value={userSearch}
          onChange={setUserSearch}
          placeholder="Search users by name or email"
          className="w-full"
        />

        {selectedUserOptions.length > 0 && (
          <div className="space-y-2">
            <div className="text-muted-foreground text-xs font-medium">
              Selected users
            </div>
            <div className="border-border divide-border divide-y overflow-hidden rounded-md border">
              {selectedUserOptions.map((option) => (
                <AudiencePrincipalRow
                  key={option.principalUrn}
                  id={`audience-selected-${option.principalUrn}`}
                  checked
                  title={option.title}
                  subtitle={option.subtitle}
                  leading={
                    <AudienceMemberAvatar
                      name={option.member?.name ?? option.title}
                      email={option.member?.email ?? option.subtitle}
                      photoUrl={option.member?.photoUrl}
                    />
                  }
                  onCheckedChange={(checked) =>
                    onTogglePrincipal(option.principalUrn, checked)
                  }
                />
              ))}
            </div>
          </div>
        )}

        <UserSearchResults
          hasSearch={hasSearch}
          hiddenResultCount={hiddenResultCount}
          results={visibleSearchResults}
          selectedAudiencePrincipalUrns={selectedAudiencePrincipalUrns}
          onTogglePrincipal={onTogglePrincipal}
        />
      </div>

      {selectedUserPrincipalUrns.length === 0 && (
        <p className="text-muted-foreground border-border border-t px-4 py-3 text-xs">
          Select at least one user to save a targeted policy.
        </p>
      )}
    </div>
  );
}

function UserSearchResults({
  hasSearch,
  hiddenResultCount,
  results,
  selectedAudiencePrincipalUrns,
  onTogglePrincipal,
}: {
  hasSearch: boolean;
  hiddenResultCount: number;
  results: AccessMember[];
  selectedAudiencePrincipalUrns: Set<string>;
  onTogglePrincipal: (principalUrn: string, checked: boolean) => void;
}) {
  if (!hasSearch) {
    return (
      <p className="text-muted-foreground text-sm">
        Search users by name or email to add them.
      </p>
    );
  }

  if (results.length === 0) {
    return (
      <p className="text-muted-foreground text-sm">No matching users to add.</p>
    );
  }

  return (
    <div className="space-y-2">
      <div className="text-muted-foreground text-xs font-medium">
        Search results
      </div>
      <div className="border-border divide-border divide-y overflow-hidden rounded-md border">
        {results.map((member) => {
          const principalUrn = member.principalUrn;
          return (
            <AudiencePrincipalRow
              key={principalUrn}
              id={`audience-result-${principalUrn}`}
              checked={selectedAudiencePrincipalUrns.has(principalUrn)}
              title={memberDisplayName(member)}
              subtitle={member.email}
              leading={
                <AudienceMemberAvatar
                  name={member.name}
                  email={member.email}
                  photoUrl={member.photoUrl}
                />
              }
              onCheckedChange={(checked) =>
                onTogglePrincipal(principalUrn, checked)
              }
            />
          );
        })}
      </div>
      {hiddenResultCount > 0 && (
        <p className="text-muted-foreground text-xs">
          Showing first {USER_SEARCH_RESULT_LIMIT} matches. Refine the search to
          narrow results.
        </p>
      )}
    </div>
  );
}

function AudienceMemberAvatar({
  name,
  email,
  photoUrl,
}: {
  name: string;
  email: string;
  photoUrl?: string;
}) {
  return (
    <Avatar className="h-7 w-7">
      {photoUrl && <AvatarImage src={photoUrl} alt={name || email} />}
      <AvatarFallback className="text-xs">
        {memberInitials({ name, email })}
      </AvatarFallback>
    </Avatar>
  );
}

function PolicyAudienceChoiceRow({
  id,
  value,
  title,
  description,
}: {
  id: string;
  value: PolicyAudienceChoice;
  title: string;
  description: string;
}) {
  return (
    <label
      htmlFor={id}
      className="hover:bg-muted/40 flex cursor-pointer gap-3 px-4 py-3"
    >
      <RadioGroupItem id={id} value={value} className="mt-0.5" />
      <span className="min-w-0">
        <span className="block text-sm font-medium">{title}</span>
        <span className="text-muted-foreground block text-xs">
          {description}
        </span>
      </span>
    </label>
  );
}

function AudiencePrincipalSection({
  title,
  children,
}: {
  title: string;
  children: ReactNode;
}) {
  return (
    <div className="border-border border-b last:border-b-0">
      <div className="bg-muted/30 border-border border-b px-4 py-2 text-xs font-medium">
        {title}
      </div>
      <div className="divide-border divide-y">{children}</div>
    </div>
  );
}

function AudiencePrincipalRow({
  id,
  checked,
  title,
  subtitle,
  leading,
  onCheckedChange,
}: {
  id: string;
  checked: boolean;
  title: string;
  subtitle: string;
  leading?: ReactNode;
  onCheckedChange: (checked: boolean) => void;
}) {
  return (
    <label
      htmlFor={id}
      className="hover:bg-muted/40 flex cursor-pointer items-start gap-3 px-4 py-3"
    >
      <Checkbox
        id={id}
        checked={checked}
        onCheckedChange={(value) => onCheckedChange(!!value)}
        className="mt-0.5"
      />
      {leading}
      <span className="min-w-0 flex-1">
        <span className="block truncate text-sm font-medium">{title}</span>
        <span className="text-muted-foreground block truncate text-xs">
          {subtitle}
        </span>
      </span>
    </label>
  );
}

/* -------------------------------------------------------------------------- */
/*  RunPanel                                                                  */
/* -------------------------------------------------------------------------- */

function RunPanel({ policy }: { policy: RiskPolicy }) {
  const {
    data: status,
    isLoading,
    refetch,
    isFetching,
  } = useRiskPoliciesStatus({ id: policy.id }, undefined, {
    refetchInterval: 5000,
  });

  const pct =
    status && status.totalMessages > 0
      ? Math.round((status.analyzedMessages / status.totalMessages) * 100)
      : 0;

  return (
    <>
      <SheetHeader className="px-6 pt-6">
        <SheetTitle>{policy.name}</SheetTitle>
        <SheetDescription>
          Analysis progress and workflow status
        </SheetDescription>
      </SheetHeader>

      <div className="flex-1 space-y-4 overflow-y-auto px-6 py-4">
        {isLoading ? (
          <div className="flex items-center justify-center py-12">
            <Loader2 className="text-muted-foreground h-5 w-5 animate-spin" />
          </div>
        ) : status ? (
          <>
            {/* Status + Version row */}
            <div className="grid grid-cols-2 gap-3">
              <div className="border-border rounded-lg border p-3">
                <p className="text-muted-foreground mb-1 text-xs font-medium">
                  Status
                </p>
                <div className="flex items-center gap-2">
                  <span
                    className={cn(
                      "inline-block h-2.5 w-2.5 rounded-full",
                      status.workflowStatus === "running" &&
                        "animate-pulse bg-green-500",
                      status.workflowStatus === "sleeping" && "bg-yellow-500",
                      status.workflowStatus === "not_started" &&
                        "bg-muted-foreground",
                    )}
                  />
                  <span className="text-sm font-medium capitalize">
                    {status.workflowStatus === "not_started"
                      ? "Idle"
                      : status.workflowStatus}
                  </span>
                </div>
              </div>
              <div className="border-border rounded-lg border p-3">
                <p className="text-muted-foreground mb-1 text-xs font-medium">
                  Version
                </p>
                <p className="text-sm font-medium">v{status.policyVersion}</p>
              </div>
            </div>

            {/* Progress */}
            <div className="border-border rounded-lg border p-4">
              <div className="mb-3 flex items-center justify-between">
                <p className="text-sm font-medium">Analysis Progress</p>
                <div className="flex items-center gap-2">
                  <Button
                    variant="tertiary"
                    onClick={() => {
                      void refetch();
                    }}
                    disabled={isFetching}
                    className="h-6 w-6"
                  >
                    <Button.Icon>
                      <RefreshCw
                        className={cn("h-3 w-3", isFetching && "animate-spin")}
                      />
                    </Button.Icon>
                  </Button>
                  <span className="text-muted-foreground text-xs font-medium">
                    {pct}%
                  </span>
                </div>
              </div>
              <div className="bg-muted mb-2 h-2 overflow-hidden rounded-full">
                <div
                  className="bg-primary h-full rounded-full transition-all duration-500"
                  style={{ width: `${pct}%` }}
                />
              </div>
              <p className="text-muted-foreground text-xs">
                {status.analyzedMessages.toLocaleString()} of{" "}
                {status.totalMessages.toLocaleString()} messages analyzed
                {status.pendingMessages > 0 && (
                  <span>
                    {" "}
                    &middot; {status.pendingMessages.toLocaleString()} pending
                  </span>
                )}
              </p>
            </div>

            {/* Findings */}
            <div className="border-border rounded-lg border p-4">
              <p className="text-muted-foreground mb-1 text-xs font-medium">
                Findings
              </p>
              <p className="text-3xl font-bold tracking-tight">
                {status.findingsCount.toLocaleString()}
              </p>
              <p className="text-muted-foreground mt-1 text-xs">
                secrets detected across all messages
              </p>
            </div>
          </>
        ) : null}
      </div>
    </>
  );
}

/* -------------------------------------------------------------------------- */
/*  ActionBadge                                                               */
/* -------------------------------------------------------------------------- */

const ACTION_BADGE_CONFIG: Record<
  PolicyAction,
  { label: string; variant: NonNullable<BadgeProps["variant"]> }
> = {
  flag: { label: "Flag", variant: "neutral" },
  block: { label: "Block", variant: "destructive" },
};

const ACTION_OPTIONS: {
  value: PolicyAction;
  title: string;
  description: string;
}[] = [
  {
    value: "flag",
    title: "Log for review",
    description: "Log findings for review without interrupting the session",
  },
  {
    value: "block",
    title: "Deny the request",
    description: "Deny prompts and tool calls that match detection rules",
  },
];

function ActionBadge({ action }: { action: PolicyAction }) {
  const config = ACTION_BADGE_CONFIG[action] ?? ACTION_BADGE_CONFIG.flag;
  return (
    <Badge variant={config.variant}>
      <Badge.Text>{config.label}</Badge.Text>
    </Badge>
  );
}

/** One session-part as a selectable card (Scope step). */
function ScopeCard({
  type,
  checked,
  onToggle,
}: {
  type: PolicyMessageType;
  checked: boolean;
  onToggle: (checked: boolean) => void;
}) {
  const meta = POLICY_MESSAGE_TYPE_META[type];
  return (
    <label
      className={cn(
        "flex cursor-pointer items-start gap-3 rounded-lg border p-3 transition-colors",
        checked
          ? "border-foreground bg-muted/40"
          : "border-border hover:bg-muted/30",
      )}
    >
      <Checkbox
        checked={checked}
        onCheckedChange={(next) => onToggle(!!next)}
        className="mt-0.5"
      />
      <div className="min-w-0">
        <div className="text-sm font-medium">{meta.label}</div>
        <div className="text-muted-foreground text-xs">{meta.description}</div>
      </div>
    </label>
  );
}

function ActionPicker({
  formAction,
  setFormAction,
  flagOnlySelected = false,
}: {
  formAction: PolicyAction;
  setFormAction: (v: PolicyAction) => void;
  flagOnlySelected?: boolean;
}) {
  const actionValue =
    flagOnlySelected && formAction === "block" ? "flag" : formAction;

  return (
    <RadioGroup
      value={actionValue}
      onValueChange={(v) => {
        if (flagOnlySelected && v === "block") {
          return;
        }
        setFormAction(v as PolicyAction);
      }}
      className="space-y-2.5"
    >
      {ACTION_OPTIONS.map((opt) => {
        const disabled = flagOnlySelected && opt.value === "block";
        const selected = actionValue === opt.value;

        return (
          <label
            key={opt.value}
            htmlFor={`action-${opt.value}`}
            className={cn(
              "flex items-start gap-3 rounded-lg border p-3.5 transition-colors",
              disabled
                ? "border-border cursor-not-allowed opacity-60"
                : selected
                  ? "border-foreground bg-muted/40 cursor-pointer"
                  : "border-border hover:bg-muted/30 cursor-pointer",
            )}
          >
            <RadioGroupItem
              value={opt.value}
              id={`action-${opt.value}`}
              className="mt-0.5"
              disabled={disabled}
            />
            <div className="flex-1">
              <div className="flex items-center gap-2">
                <ActionBadge action={opt.value} />
                <span className="text-sm font-medium">{opt.title}</span>
              </div>
              <div className="text-muted-foreground mt-1.5 text-xs">
                {opt.description}
              </div>
              {disabled && (
                <div className="text-destructive mt-1 text-xs font-medium">
                  Destructive Tools and Destructive CLI Commands support
                  flagging only.
                </div>
              )}
            </div>
          </label>
        );
      })}
    </RadioGroup>
  );
}

/* -------------------------------------------------------------------------- */
/*  RuleSelectList                                                             */
/* -------------------------------------------------------------------------- */

/** A collapsible checkbox list of the org's custom rules. Used twice in the
 *  standard-policy wizard: in the Detect step to attach rules as detectors, and
 *  in the Scope step to attach them as exemptions. The two lists drive disjoint
 *  id sets (custom_rule_ids vs exempt_rule_ids); the caller's onToggleRule keeps
 *  them mutually exclusive. */
function RuleSelectList({
  title,
  description,
  idPrefix,
  customRules,
  selectedRuleIds,
  onToggleRule,
  expanded,
  onToggle,
}: {
  title: string;
  description: ReactNode;
  idPrefix: string;
  customRules: ReturnType<typeof useDetectionRulesStore>["customRules"];
  selectedRuleIds: Set<string>;
  onToggleRule: (ruleId: string, checked: boolean) => void;
  expanded: boolean;
  onToggle: () => void;
}) {
  const selectedCount = customRules.filter((r) =>
    selectedRuleIds.has(r.id),
  ).length;
  return (
    <div className="space-y-3">
      <button
        type="button"
        onClick={onToggle}
        className="flex w-full items-center gap-2"
      >
        <ChevronRight
          className={cn(
            "text-muted-foreground h-4 w-4 shrink-0 transition-transform",
            expanded && "rotate-90",
          )}
        />
        <Label className="cursor-pointer text-sm font-medium">{title}</Label>
        {selectedCount > 0 && (
          <Badge variant="neutral">
            <Badge.Text>{selectedCount} selected</Badge.Text>
          </Badge>
        )}
      </button>
      {expanded && (
        <div className="border-border divide-border divide-y rounded-lg border">
          <p className="text-muted-foreground px-4 py-3 text-xs">
            {description}
          </p>
          <div className="space-y-2 px-4 py-3">
            {customRules.map((rule) => (
              <div key={rule.id} className="flex items-center gap-3 py-1">
                <Checkbox
                  id={`${idPrefix}-${rule.id}`}
                  checked={selectedRuleIds.has(rule.id)}
                  onCheckedChange={(next) => onToggleRule(rule.id, !!next)}
                />
                <label
                  htmlFor={`${idPrefix}-${rule.id}`}
                  className="min-w-0 flex-1 cursor-pointer truncate text-xs"
                >
                  <span className="text-foreground">
                    {rule.title || rule.id}
                  </span>
                  <span className="text-muted-foreground ml-2 font-mono text-[10px]">
                    {rule.id}
                  </span>
                </label>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}
