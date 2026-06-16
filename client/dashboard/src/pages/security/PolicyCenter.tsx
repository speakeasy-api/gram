import { InsightsConfig } from "@/components/insights-dock";
import { INSIGHTS_SUGGESTIONS } from "@/lib/insights-suggestions";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { Slider } from "@/components/ui/slider";
import { Switch } from "@/components/ui/switch";
import { TextArea } from "@/components/ui/textarea";
import { SimpleTooltip } from "@/components/ui/tooltip";
import {
  OnboardingStepper,
  type Step,
} from "@/pages/setup/components/onboarding-stepper";
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetDescription,
} from "@/components/ui/sheet";
import { Dialog } from "@/components/ui/dialog";
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
} from "lucide-react";
import {
  useState,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  type ReactNode,
} from "react";
import { useQueryState } from "nuqs";
import { useQueryClient } from "@tanstack/react-query";
import {
  invalidateAllRiskListPolicies,
  useRiskCreatePolicyMutation,
  useRiskListPolicies,
  useRiskPoliciesDeleteMutation,
  useRiskPoliciesUpdateMutation,
} from "@gram/client/react-query/index.js";
import {
  useRiskPoliciesStatus,
  invalidateAllRiskPoliciesStatus,
} from "@gram/client/react-query/riskPoliciesStatus.js";
import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
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
import {
  ruleConditions,
  ruleRequiredMessageTypes,
  useDetectionRulesStore,
} from "./detection-rules-data";
import { Alert, AlertDescription } from "@/components/ui/alert";
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
  POLICY_WIZARD_STEPS[1]!,
  POLICY_WIZARD_STEPS[2]!,
  POLICY_WIZARD_STEPS[3]!,
];

/** Shared wizard chrome: the left step rail + the paged content column. The
 *  footer (Back/Continue/Create) lives in the sheet, driven by the parent. */
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
    <div className="flex gap-8 py-4">
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

/** Built-in detector categories that only produce findings when Speakeasy hooks
 *  are installed on the agent (no rule list to customize). */
const HOOK_REQUIRED_CATEGORIES: Set<RuleCategory> = new Set([
  "shadow_mcp",
  "destructive_tool",
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

type PolicyRow = { kind: PolicyKind; policy: RiskPolicy };

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
 * - `promptInjectionRules` stays empty for backward compatibility — the
 *   detection engine (deberta classifier vs L0 regex) is chosen per-org
 *   via a feature flag, not by the policy author. */
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

function promptPolicyName(prompt: string): string {
  return prompt.trim().replace(/\s+/g, " ").slice(0, 60) || "Prompt Policy";
}

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

  const [sheetOpen, setSheetOpen] = useState(false);
  const [editingPolicy, setEditingPolicy] = useState<RiskPolicy | null>(null);
  const [createStep, setCreateStep] = useState<"type" | "details">("details");
  // Active step in the standard-policy guided flow (0=Detect…3=Review).
  const [wizardStep, setWizardStep] = useState(0);
  const [formPolicyKind, setFormPolicyKind] = useState<PolicyKind>("risk");
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
  // Custom rules attached as exemptions (risk_policies.exempt_rule_ids): when one
  // matches a message, the whole policy is skipped for that message. Disjoint
  // from selectedCustomRuleIds (detectors) — a rule is one or the other here.
  const [exemptRuleIds, setExemptRuleIds] = useState<Set<string>>(
    new Set<string>(),
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
  // Fail-open (true) is the server default: allow the message when the judge errors.
  const [formFailOpen, setFormFailOpen] = useState(true);

  const [runPanelPolicy, setRunPanelPolicy] = useState<RiskPolicy | null>(null);

  const [activeTab, setActiveTab] = useState<"policies" | "exclusions">(
    "policies",
  );
  const [exclusionSheet, setExclusionSheet] =
    useState<ExclusionSheetState | null>(null);

  // Deep-link support: `?policy=<id>` opens that policy's edit sheet. The
  // command palette uses this since policies have no per-item route. Declared
  // here (above the mutations) so save handlers can clear it on programmatic
  // close — Radix's onOpenChange only fires for user-initiated closes.
  const [policyParam, setPolicyParam] = useQueryState("policy");
  const openedPolicyRef = useRef<string | null>(null);
  const clearPolicyDeepLink = useCallback(() => {
    openedPolicyRef.current = null;
    void setPolicyParam(null);
  }, [setPolicyParam]);

  const invalidate = useCallback(() => {
    void invalidateAllRiskListPolicies(queryClient);
    void invalidateAllRiskPoliciesStatus(queryClient);
  }, [queryClient]);

  const createMutation = useRiskCreatePolicyMutation({
    onSuccess: () => {
      invalidate();
      setSheetOpen(false);
      clearPolicyDeepLink();
    },
  });

  const updateMutation = useRiskPoliciesUpdateMutation({
    onSuccess: () => {
      invalidate();
      setSheetOpen(false);
      clearPolicyDeepLink();
    },
  });

  const deleteMutation = useRiskPoliciesDeleteMutation({
    onSuccess: invalidate,
  });

  const configurePolicyKind = (kind: PolicyKind) => {
    setFormPolicyKind(kind);
    setSelectedMessageTypes(new Set(ALL_POLICY_MESSAGE_TYPES));
    setFormAction("flag");
    setFormAutoName(true);
  };

  const handleCreate = (kind?: PolicyKind) => {
    const nextKind = kind ?? "risk";
    setEditingPolicy(null);
    setCreateStep(kind || !nlEnabled ? "details" : "type");
    setWizardStep(0);
    setFormPolicyKind(nextKind);
    setFormName("");
    setFormEnabled(true);
    setFormPromptInstruction("");
    setSelectedCategories(new Set<RuleCategory>());
    setDisabledRules(new Set());
    setSelectedCustomRuleIds(new Set<string>());
    setExemptRuleIds(new Set<string>());
    setSelectedMessageTypes(new Set(ALL_POLICY_MESSAGE_TYPES));
    setFormAction("flag");
    setFormAutoName(true);
    setFormUserMessage("");
    setFormModel("");
    setFormTemperature(DEFAULT_JUDGE_TEMPERATURE);
    setFormFailOpen(true);
    setSheetOpen(true);
  };

  const handleChoosePolicyKind = (kind: PolicyKind) => {
    configurePolicyKind(kind);
    setCreateStep("details");
  };

  // Memoized so the deep-link effect below can depend on it without re-running
  // every render. Body references only module-level helpers + stable setters,
  // so the empty dependency array is correct.
  const handleEdit = useCallback((policy: RiskPolicy) => {
    const isPrompt = isPromptPolicy(policy);
    const kind: PolicyKind = isPrompt ? "prompt" : "risk";
    setEditingPolicy(policy);
    setCreateStep("details");
    setWizardStep(0);
    setFormPolicyKind(kind);
    setFormName(policy.name);
    setFormEnabled(policy.enabled);
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
      setSheetOpen(true);
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
    setDisabledRules(new Set(policy.disabledRules ?? []));
    setSelectedCustomRuleIds(new Set<string>(customRuleIds));
    setExemptRuleIds(new Set<string>(policy.exemptRuleIds ?? []));
    setSelectedMessageTypes(policyMessageTypesForForm(policy.messageTypes));
    setFormAction((policy.action as PolicyAction) ?? "flag");
    setFormAutoName(policy.autoName ?? true);
    setFormUserMessage(policy.userMessage ?? "");
    setSheetOpen(true);
  }, []);

  // Open the deep-linked policy once its data has loaded. Guarded by a ref so it
  // fires once per id (not on every policies re-fetch). The ref is marked as
  // handled even when the id doesn't resolve, so a stale/invalid id doesn't
  // re-trigger the lookup on every subsequent `data` change.
  useEffect(() => {
    if (!policyParam || isLoading) return;
    if (openedPolicyRef.current === policyParam) return;
    // Read from the stable react-query `data` (not the per-render `policies`
    // array) so the effect doesn't re-run every render.
    const policy = data?.policies?.find((p) => p.id === policyParam);
    openedPolicyRef.current = policyParam;
    if (policy) {
      handleEdit(policy);
    }
  }, [policyParam, isLoading, data, handleEdit]);

  const handleSave = () => {
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
      const promptMessageTypes =
        policyMessageTypesForPayload(selectedMessageTypes);
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
              ...(modelConfig ? { modelConfig } : {}),
              ...userMessagePayload,
            },
          },
        });
      }
      return;
    }

    const messageTypes = policyMessageTypesForPayload(selectedMessageTypes);
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
            exemptRuleIds: [...exemptRuleIds],
            messageTypes,
            action,
            autoName: formAutoName,
            userMessage: formUserMessage,
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
            exemptRuleIds: [...exemptRuleIds],
            messageTypes,
            action,
            autoName: formAutoName,
            ...(formUserMessage.trim() ? { userMessage: formUserMessage } : {}),
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
    "Available risk tools: listRiskPolicies, getRiskPolicy, getRiskCapabilities, getRiskPolicyStatus, listRiskResultsForAgent (finding-level with match redaction), listRiskResultsByChat, listShadowMCPApprovals.",
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

  const isChoosingPolicyKind =
    !editingPolicy && nlEnabled && createStep === "type";

  // Footer behaviour for the guided flow. Both standard and prompt policies now
  // page through steps (Continue, then Create/Update); only the type chooser is
  // exempt.
  const isWizard = !isChoosingPolicyKind;
  const isRiskWizard = isWizard && formPolicyKind === "risk";
  const wizardSteps =
    formPolicyKind === "prompt" ? PROMPT_WIZARD_STEPS : POLICY_WIZARD_STEPS;
  const isLastWizardStep = wizardStep === wizardSteps.length - 1;
  const showWizardContinue = isWizard && !isLastWizardStep;
  const mutationPending = createMutation.isPending || updateMutation.isPending;
  const continueDisabled =
    (wizardStep === 0 &&
      (formPolicyKind === "prompt"
        ? !formPromptInstruction.trim()
        : selectedCategories.size === 0 && selectedCustomRuleIds.size === 0)) ||
    (wizardStep === 1 && selectedMessageTypes.size === 0);
  const saveDisabled =
    (formPolicyKind === "prompt" && !formPromptInstruction.trim()) ||
    // A standard policy needs at least one detector or custom rule (the step-0
    // gate, re-checked here since free-jump can skip it).
    (isRiskWizard &&
      selectedCategories.size === 0 &&
      selectedCustomRuleIds.size === 0) ||
    (!formAutoName && !formName.trim()) ||
    selectedMessageTypes.size === 0 ||
    mutationPending;
  const showFooterBack =
    (isWizard && wizardStep > 0) || (!editingPolicy && nlEnabled);
  const onFooterBack = () => {
    if (isWizard && wizardStep > 0) {
      setWizardStep(wizardStep - 1);
    } else {
      setCreateStep("type");
    }
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

        {/* Edit/Create Sheet */}
        <Dialog
          open={sheetOpen}
          onOpenChange={(open) => {
            setSheetOpen(open);
            // Drop the deep-link param so the dialog doesn't reopen and the same
            // id can be deep-linked again later.
            if (!open) clearPolicyDeepLink();
          }}
        >
          <Dialog.Content
            className={cn(
              "flex max-h-[85vh] flex-col gap-0 overflow-hidden p-0",
              // The guided create/edit flow is a centered wizard that needs room
              // for the step rail; the type chooser stays compact.
              isWizard ? "sm:max-w-5xl" : "sm:max-w-lg",
            )}
          >
            <Dialog.Header className="px-6 pt-6">
              <Dialog.Title>{sheetTitle}</Dialog.Title>
              <Dialog.Description>{sheetDescription}</Dialog.Description>
            </Dialog.Header>
            <div className="flex-1 overflow-y-auto px-6">
              <div className="space-y-6 py-4">
                {isChoosingPolicyKind ? (
                  <PolicyKindChoice onSelect={handleChoosePolicyKind} />
                ) : formPolicyKind === "risk" ? (
                  <PolicySheetBody
                    wizardStep={wizardStep}
                    setWizardStep={setWizardStep}
                    formName={formName}
                    setFormName={setFormName}
                    formEnabled={formEnabled}
                    setFormEnabled={setFormEnabled}
                    selectedCategories={selectedCategories}
                    setSelectedCategories={setSelectedCategories}
                    disabledRules={disabledRules}
                    setDisabledRules={setDisabledRules}
                    customRules={customRules}
                    selectedCustomRuleIds={selectedCustomRuleIds}
                    setSelectedCustomRuleIds={setSelectedCustomRuleIds}
                    exemptRuleIds={exemptRuleIds}
                    setExemptRuleIds={setExemptRuleIds}
                    selectedMessageTypes={selectedMessageTypes}
                    setSelectedMessageTypes={setSelectedMessageTypes}
                    formAction={formAction}
                    setFormAction={setFormAction}
                    formAutoName={formAutoName}
                    setFormAutoName={setFormAutoName}
                    formUserMessage={formUserMessage}
                    setFormUserMessage={setFormUserMessage}
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
                    selectedMessageTypes={selectedMessageTypes}
                    setSelectedMessageTypes={setSelectedMessageTypes}
                  />
                )}
              </div>
            </div>
            {!isChoosingPolicyKind && (
              <Dialog.Footer className="border-border flex-row items-center justify-between border-t px-6 py-4">
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
              </Dialog.Footer>
            )}
          </Dialog.Content>
        </Dialog>

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
  const summaryScopes =
    selectedMessageTypes.size === ALL_POLICY_MESSAGE_TYPES.length
      ? ["All session parts"]
      : ALL_POLICY_MESSAGE_TYPES.filter((t) =>
          selectedMessageTypes.has(t as PolicyMessageType),
        ).map((t) => POLICY_MESSAGE_TYPE_META[t as PolicyMessageType].label);

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
          <div>
            <button
              type="button"
              onClick={() => setAdvancedOpen((v) => !v)}
              className="text-muted-foreground hover:text-foreground flex items-center gap-1.5 text-xs font-medium"
            >
              <ChevronRight
                className={cn(
                  "h-3.5 w-3.5 transition-transform",
                  advancedOpen && "rotate-90",
                )}
              />
              Advanced — judge model &amp; behavior
            </button>
            {advancedOpen && (
              <div className="mt-3">
                <JudgeConfigSection
                  formModel={formModel}
                  setFormModel={setFormModel}
                  formTemperature={formTemperature}
                  setFormTemperature={setFormTemperature}
                  formFailOpen={formFailOpen}
                  setFormFailOpen={setFormFailOpen}
                />
              </div>
            )}
          </div>
        </div>
      )}

      {wizardStep === 1 && (
        <div className="space-y-6">
          <WizardStepHeading
            title="Where should it evaluate?"
            description="Narrow the scope to control cost — a prompt policy runs the LLM judge on each in-scope message."
          />
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            {ALL_POLICY_MESSAGE_TYPES.map((type) => (
              <ScopeCard
                key={type}
                type={type as PolicyMessageType}
                checked={selectedMessageTypes.has(type as PolicyMessageType)}
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
            An LLM judge checks each matching message against your prompt.
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
          Prompt-based policies use an LLM judge, so each evaluated message adds
          some latency versus standard detection rules. The judge sees one
          message at a time (a tool call and its inputs, or message content),
          never a whole conversation. It runs on the message types you select
          under Applies To.
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
  disabledRules,
  setDisabledRules,
  customRules,
  selectedCustomRuleIds,
  setSelectedCustomRuleIds,
  exemptRuleIds,
  setExemptRuleIds,
  selectedMessageTypes,
  setSelectedMessageTypes,
  formAction,
  setFormAction,
  formAutoName,
  setFormAutoName,
  formUserMessage,
  setFormUserMessage,
}: {
  wizardStep: number;
  setWizardStep: (v: number) => void;
  formName: string;
  setFormName: (v: string) => void;
  formEnabled: boolean;
  setFormEnabled: (v: boolean) => void;
  selectedCategories: Set<RuleCategory>;
  setSelectedCategories: (v: Set<RuleCategory>) => void;
  disabledRules: Set<string>;
  setDisabledRules: (v: Set<string>) => void;
  customRules: ReturnType<typeof useDetectionRulesStore>["customRules"];
  selectedCustomRuleIds: Set<string>;
  setSelectedCustomRuleIds: (v: Set<string>) => void;
  exemptRuleIds: Set<string>;
  setExemptRuleIds: (v: Set<string>) => void;
  selectedMessageTypes: Set<PolicyMessageType>;
  setSelectedMessageTypes: (v: Set<PolicyMessageType>) => void;
  formAction: PolicyAction;
  setFormAction: (v: PolicyAction) => void;
  formAutoName: boolean;
  setFormAutoName: (v: boolean) => void;
  formUserMessage: string;
  setFormUserMessage: (v: string) => void;
}) {
  // The org's custom rules collapse into their own section; the Customize sheet
  // opens for one detector category at a time.
  const [detectionExpanded, setDetectionExpanded] = useState(true);
  // Exemptions are an advanced scope concern; collapsed unless the policy
  // already has some attached.
  const [exemptionsExpanded, setExemptionsExpanded] = useState(
    () => exemptRuleIds.size > 0,
  );
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

  // A custom rule is either a detector or an exemption in a given policy, never
  // both. Toggling one side removes the id from the other so the two sets stay
  // disjoint, matching the backend's custom_rule_ids / exempt_rule_ids columns.
  const toggleDetector = (ruleId: string, checked: boolean) => {
    const next = new Set(selectedCustomRuleIds);
    if (checked) {
      next.add(ruleId);
      if (exemptRuleIds.has(ruleId)) {
        const nextExempt = new Set(exemptRuleIds);
        nextExempt.delete(ruleId);
        setExemptRuleIds(nextExempt);
      }
    } else {
      next.delete(ruleId);
    }
    setSelectedCustomRuleIds(next);
  };
  const toggleExemption = (ruleId: string, checked: boolean) => {
    const next = new Set(exemptRuleIds);
    if (checked) {
      next.add(ruleId);
      if (selectedCustomRuleIds.has(ruleId)) {
        const nextDetectors = new Set(selectedCustomRuleIds);
        nextDetectors.delete(ruleId);
        setSelectedCustomRuleIds(nextDetectors);
      }
    } else {
      next.delete(ruleId);
    }
    setExemptRuleIds(next);
  };

  // Coverage gaps: attached custom rules whose targets imply message types the
  // policy scope excludes — those rules silently never run. Built-in detectors
  // are type-agnostic, so they never gap. Surfaced in the Scope step.
  const coverageGaps = customRules
    .filter((r) => selectedCustomRuleIds.has(r.id))
    .map((r) => ({
      title: r.title || r.id,
      missing: ruleRequiredMessageTypes(ruleConditions(r)).filter(
        (t) => !selectedMessageTypes.has(t),
      ),
    }))
    .filter((gap) => gap.missing.length > 0);
  const missingScopeTypes = [
    ...new Set(coverageGaps.flatMap((gap) => gap.missing)),
  ];
  const missingScopeLabels = missingScopeTypes
    .map((t) => POLICY_MESSAGE_TYPE_META[t].label)
    .join(", ");

  // Review-step summary chips.
  const summaryDetectors = ALL_CATEGORIES.filter((c) =>
    selectedCategories.has(c),
  ).map((c) => RULE_CATEGORY_META[c].label);
  const summaryScopes =
    selectedMessageTypes.size === ALL_POLICY_MESSAGE_TYPES.length
      ? ["All session parts"]
      : ALL_POLICY_MESSAGE_TYPES.filter((t) =>
          selectedMessageTypes.has(t as PolicyMessageType),
        ).map((t) => POLICY_MESSAGE_TYPE_META[t as PolicyMessageType].label);

  return (
    <>
      <WizardShell
        steps={POLICY_WIZARD_STEPS}
        currentStep={wizardStep}
        setCurrentStep={setWizardStep}
      >
        {wizardStep === 0 && (
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

        {wizardStep === 1 && (
          <div className="space-y-6">
            <WizardStepHeading
              title="Where should it evaluate?"
              description="Leave all four on to apply everywhere. Narrow the scope to reduce noise or cost."
            />
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              {ALL_POLICY_MESSAGE_TYPES.map((type) => (
                <ScopeCard
                  key={type}
                  type={type as PolicyMessageType}
                  checked={selectedMessageTypes.has(type as PolicyMessageType)}
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
            {coverageGaps.length > 0 && (
              <Alert variant="warning">
                <AlertDescription>
                  <p className="font-medium">
                    {coverageGaps.length === 1
                      ? "1 attached rule targets message types outside this scope and won't run:"
                      : `${coverageGaps.length} attached rules target message types outside this scope and won't run:`}
                  </p>
                  <ul className="mt-1 list-disc space-y-0.5 pl-4">
                    {coverageGaps.map((gap) => (
                      <li key={gap.title}>
                        {gap.title} — needs{" "}
                        {gap.missing
                          .map((t) => POLICY_MESSAGE_TYPE_META[t].label)
                          .join(", ")}
                      </li>
                    ))}
                  </ul>
                  <Button
                    variant="secondary"
                    className="mt-2"
                    onClick={() => {
                      const next = new Set(selectedMessageTypes);
                      for (const t of missingScopeTypes) next.add(t);
                      setSelectedMessageTypes(next);
                    }}
                  >
                    <Button.Text>Include {missingScopeLabels}</Button.Text>
                  </Button>
                </AlertDescription>
              </Alert>
            )}
            {customRules.length > 0 && (
              <RuleSelectList
                title="Exemptions"
                description={
                  <>
                    Skip this entire policy for a message when one of these
                    custom rules matches it (an allowlist). A rule used here
                    can't also be a detector.
                  </>
                }
                idPrefix="exempt"
                customRules={customRules}
                selectedRuleIds={exemptRuleIds}
                onToggleRule={toggleExemption}
                expanded={exemptionsExpanded}
                onToggle={() => setExemptionsExpanded((v) => !v)}
              />
            )}
          </div>
        )}

        {wizardStep === 2 && (
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

        {wizardStep === 3 && (
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
                {exemptRuleIds.size > 0 && (
                  <SummaryRow
                    label="Exemptions"
                    chips={[`${exemptRuleIds.size} attached`]}
                  />
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
