import { InsightsConfig } from "@/components/insights-sidebar";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Badge } from "@/components/ui/badge";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { Switch } from "@/components/ui/switch";
import { TextArea } from "@/components/ui/textarea";
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetDescription,
  SheetFooter,
} from "@/components/ui/sheet";
import { Type } from "@/components/ui/type";
import {
  Button,
  type Column,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
  Icon,
  Table,
} from "@speakeasy-api/moonshine";
import type { IconName } from "@speakeasy-api/moonshine";
import {
  Plus,
  Shield,
  Ellipsis,
  Loader2,
  ChevronRight,
  RefreshCw,
} from "lucide-react";
import { useState, useCallback } from "react";
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
  POLICY_INPUT_TYPE_META,
  type RuleCategory,
  type PolicyAction,
  type PolicyInputType,
} from "./policy-data";
import { cn } from "@/lib/utils";
import { ruleIdToPresidioEntity } from "./rule-ids";
import { useDetectionRulesStore } from "./detection-rules-data";

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

const ALL_POLICY_INPUT_TYPES = Object.keys(
  POLICY_INPUT_TYPE_META,
) as Array<PolicyInputType>;

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

/** Map sources to display categories for the table row badges. */
function sourcesToCategories(
  sources: string[],
  presidioEntities?: string[],
): RuleCategory[] {
  return [...policyToCategories(sources, presidioEntities)];
}

function policyInputTypesForForm(inputTypes?: string[]): Set<PolicyInputType> {
  if (!inputTypes?.length) {
    return new Set(ALL_POLICY_INPUT_TYPES);
  }

  return new Set(
    inputTypes.filter((type): type is PolicyInputType =>
      ALL_POLICY_INPUT_TYPES.includes(type as PolicyInputType),
    ),
  );
}

function policyInputTypesForPayload(
  selectedInputTypes: Set<PolicyInputType>,
): PolicyInputType[] {
  const orderedTypes = ALL_POLICY_INPUT_TYPES.filter((type) =>
    selectedInputTypes.has(type),
  );
  if (orderedTypes.length === ALL_POLICY_INPUT_TYPES.length) {
    return [];
  }
  return orderedTypes;
}

function policyInputTypesForDisplay(inputTypes?: string[]): PolicyInputType[] {
  return [...policyInputTypesForForm(inputTypes)];
}

function inputTypesSummary(selectedInputTypes: Set<PolicyInputType>): string {
  if (selectedInputTypes.size === ALL_POLICY_INPUT_TYPES.length) {
    return "All types";
  }

  return `${selectedInputTypes.size} of ${ALL_POLICY_INPUT_TYPES.length} types selected`;
}

export default function PolicyCenter() {
  return (
    <RequireScope scope="org:admin" level="page">
      <PolicyCenterContent />
    </RequireScope>
  );
}

function PolicyCenterContent() {
  const queryClient = useQueryClient();
  const { data, isLoading } = useRiskListPolicies();
  const policies = data?.policies ?? [];

  const { customRules } = useDetectionRulesStore();

  const [sheetOpen, setSheetOpen] = useState(false);
  const [editingPolicy, setEditingPolicy] = useState<RiskPolicy | null>(null);
  const [formName, setFormName] = useState("");
  const [formEnabled, setFormEnabled] = useState(true);
  const [selectedCategories, setSelectedCategories] = useState<
    Set<RuleCategory>
  >(new Set<RuleCategory>(["secrets", "pii"]));
  const [disabledRules, setDisabledRules] = useState<Set<string>>(new Set());
  const [selectedCustomRuleIds, setSelectedCustomRuleIds] = useState<
    Set<string>
  >(new Set<string>());
  const [selectedInputTypes, setSelectedInputTypes] = useState<
    Set<PolicyInputType>
  >(new Set(ALL_POLICY_INPUT_TYPES));
  const [formAction, setFormAction] = useState<PolicyAction>("flag");
  const [formAutoName, setFormAutoName] = useState(true);
  const [formUserMessage, setFormUserMessage] = useState("");

  const [runPanelPolicy, setRunPanelPolicy] = useState<RiskPolicy | null>(null);

  const invalidate = useCallback(() => {
    invalidateAllRiskListPolicies(queryClient);
    invalidateAllRiskPoliciesStatus(queryClient);
  }, [queryClient]);

  const createMutation = useRiskCreatePolicyMutation({
    onSuccess: () => {
      invalidate();
      setSheetOpen(false);
    },
  });

  const updateMutation = useRiskPoliciesUpdateMutation({
    onSuccess: () => {
      invalidate();
      setSheetOpen(false);
    },
  });

  const deleteMutation = useRiskPoliciesDeleteMutation({
    onSuccess: invalidate,
  });

  const handleCreate = () => {
    setEditingPolicy(null);
    setFormName("");
    setFormEnabled(true);
    setSelectedCategories(new Set<RuleCategory>(["secrets", "pii"]));
    setDisabledRules(new Set());
    setSelectedCustomRuleIds(new Set<string>());
    setSelectedInputTypes(new Set(ALL_POLICY_INPUT_TYPES));
    setFormAction("flag");
    setFormAutoName(true);
    setFormUserMessage("");
    setSheetOpen(true);
  };

  const handleEdit = (policy: RiskPolicy) => {
    const customRuleIds = policy.customRuleIds ?? [];
    setEditingPolicy(policy);
    setFormName(policy.name);
    setFormEnabled(policy.enabled);
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
    setSelectedInputTypes(policyInputTypesForForm(policy.inputTypes));
    setFormAction((policy.action as PolicyAction) ?? "flag");
    setFormAutoName(policy.autoName ?? true);
    setFormUserMessage(policy.userMessage ?? "");
    setSheetOpen(true);
  };

  const handleSave = () => {
    const {
      sources,
      presidioEntities,
      promptInjectionRules,
      disabledRules: payloadDisabled,
    } = categoriesToPayload(selectedCategories, disabledRules);
    const inputTypes = policyInputTypesForPayload(selectedInputTypes);
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
            inputTypes,
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
            inputTypes,
            action,
            autoName: formAutoName,
            ...(formUserMessage.trim() ? { userMessage: formUserMessage } : {}),
          },
        },
      });
    }
  };

  const handleDelete = (id: string) => {
    deleteMutation.mutate({ request: { id } });
  };

  const handleToggle = (policy: RiskPolicy, enabled: boolean) => {
    updateMutation.mutate({
      request: {
        updateRiskPolicyRequestBody: {
          id: policy.id,
          name: policy.name,
          enabled,
          inputTypes: policy.inputTypes ?? [],
        },
      },
    });
  };

  if (isLoading) {
    return (
      <Page>
        <Page.Header>
          <Page.Header.Breadcrumbs />
        </Page.Header>
        <Page.Body>
          <div className="flex items-center justify-center py-20">
            <Loader2 className="text-muted-foreground h-5 w-5 animate-spin" />
          </div>
        </Page.Body>
      </Page>
    );
  }

  if (policies.length === 0) {
    return (
      <Page>
        <Page.Header>
          <Page.Header.Breadcrumbs />
        </Page.Header>
        <Page.Body>
          <div className="bg-muted/20 flex flex-col items-center justify-center rounded-xl border border-dashed px-8 py-16">
            <div className="bg-muted/50 mb-4 flex h-12 w-12 items-center justify-center rounded-full">
              <Shield className="text-muted-foreground h-6 w-6" />
            </div>
            <Type variant="subheading" className="mb-1">
              No Risk Policies
            </Type>
            <Type small muted className="mb-4 max-w-md text-center">
              Risk policies scan your chat messages for secrets and sensitive
              data. Create your first policy to get started.
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
        </Page.Body>
      </Page>
    );
  }

  const enabledPolicies = policies.filter((p) => p.enabled);
  const insightsContext = [
    "Page: Policy Center.",
    `Total policies: ${policies.length}, enabled: ${enabledPolicies.length}.`,
    `Policy actions: ${policies.map((p) => `${p.name} (${p.action})`).join(", ") || "none"}.`,
    "Available risk tools: listRiskPolicies, getRiskPolicy, getRiskCapabilities, getRiskPolicyStatus, listRiskResultsForAgent (finding-level with match redaction), listRiskResultsByChat, listShadowMCPApprovals.",
    "Never echo match_redacted values verbatim. Refer to findings by rule_id and source.",
  ].join(" ");

  const insightsSuggestions = [
    {
      title: "Policy status snapshot",
      label: "what's running and what's stuck",
      prompt:
        "For each policy returned by listRiskPolicies, call getRiskPolicyStatus and report: enabled flag, action (flag vs block), total messages, pending messages, and workflow state. Flag any policy with non-zero pending messages.",
    },
    {
      title: "Quiet policies",
      label: "policies with no recent findings",
      prompt:
        "Identify policies that have not produced any findings in the last 30 days. Use listRiskResultsForAgent with policy_id to check each policy. Report by name and last-seen finding date.",
    },
    {
      title: "Coverage by source",
      label: "what's each source catching",
      prompt:
        "Group findings by source (gitleaks, presidio, prompt_injection, shadow_mcp, destructive_tool) over the last 7 days using listRiskResultsForAgent. Report counts and the top rule_id per source family.",
    },
    {
      title: "Capabilities check",
      label: "what detectors are available",
      prompt:
        "Call getRiskCapabilities and tell me which detection backends are configured on this server (e.g. prompt-injection ML classifier).",
    },
  ];

  const policyColumns: Column<RiskPolicy>[] = [
    {
      key: "name",
      header: "Name",
      width: "1fr",
      render: (policy) => <span className="font-medium">{policy.name}</span>,
    },
    {
      key: "action",
      header: "Action",
      width: "0.5fr",
      render: (policy) => (
        <ActionBadge action={(policy.action as PolicyAction) ?? "flag"} />
      ),
    },
    {
      key: "sources",
      header: "Categories",
      width: "2fr",
      render: (policy) => {
        const categories = sourcesToCategories(
          policy.sources,
          policy.presidioEntities,
        );
        if (policy.customRuleIds?.length) {
          categories.push("custom");
        }

        return (
          <div className="flex flex-wrap gap-1">
            {categories.map((cat) => (
              <Badge key={cat} variant="secondary">
                {RULE_CATEGORY_META[cat].label}
              </Badge>
            ))}
          </div>
        );
      },
    },
    {
      key: "inputTypes",
      header: "Message Types",
      width: "2.1fr",
      render: (policy) => {
        const types = policyInputTypesForDisplay(policy.inputTypes);

        return (
          <div className="flex flex-wrap gap-1">
            {types.map((type) => (
              <Badge key={type} variant="secondary">
                {POLICY_INPUT_TYPE_META[type].label}
              </Badge>
            ))}
          </div>
        );
      },
    },
    {
      key: "enabled",
      header: "Status",
      width: "0.5fr",
      render: (policy) => (
        <div onClick={(e) => e.stopPropagation()}>
          <Switch
            checked={policy.enabled}
            onCheckedChange={(checked) => handleToggle(policy, checked)}
          />
        </div>
      ),
    },
    {
      key: "actions",
      header: "",
      width: "0.3fr",
      render: (policy) => (
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
                onSelect={() => setTimeout(() => setRunPanelPolicy(policy), 0)}
              >
                View Progress
              </DropdownMenuItem>
              <DropdownMenuItem
                className="text-destructive focus:text-destructive cursor-pointer"
                onSelect={() => handleDelete(policy.id)}
              >
                Delete
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      ),
    },
  ];

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <InsightsConfig
          contextInfo={insightsContext}
          suggestions={insightsSuggestions}
          title="Policy insights"
          subtitle="Ask about policy status, coverage, and detector capabilities. Match content is redacted before it reaches the assistant."
        />
        <div className="flex items-center justify-between">
          <div>
            <h2 className="text-lg font-semibold">Risk Policies</h2>
            <p className="text-muted-foreground text-sm">
              Configure risk analysis rules to detect secrets and sensitive
              information in agent session interactions.
            </p>
          </div>
          <Button onClick={handleCreate}>
            <Button.LeftIcon>
              <Plus className="mr-2 h-4 w-4" />
            </Button.LeftIcon>
            <Button.Text>New Policy</Button.Text>
          </Button>
        </div>

        <Table
          columns={policyColumns}
          data={policies}
          rowKey={(policy) => policy.id}
          onRowClick={handleEdit}
        />

        {/* Edit/Create Sheet */}
        <Sheet open={sheetOpen} onOpenChange={setSheetOpen}>
          <SheetContent className="flex flex-col overflow-y-auto sm:max-w-lg">
            <SheetHeader className="px-6 pt-6">
              <SheetTitle>
                {editingPolicy ? "Edit Policy" : "New Policy"}
              </SheetTitle>
              <SheetDescription>
                {editingPolicy
                  ? "Update the risk analysis policy configuration."
                  : "Create a new risk analysis policy to scan chat messages."}
              </SheetDescription>
            </SheetHeader>
            <div className="flex-1 overflow-y-auto px-6">
              <PolicySheetBody
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
                selectedInputTypes={selectedInputTypes}
                setSelectedInputTypes={setSelectedInputTypes}
                formAction={formAction}
                setFormAction={setFormAction}
                formAutoName={formAutoName}
                setFormAutoName={setFormAutoName}
                formUserMessage={formUserMessage}
                setFormUserMessage={setFormUserMessage}
              />
            </div>
            <SheetFooter className="px-6 pb-6">
              <Button
                onClick={handleSave}
                disabled={
                  (!formAutoName && !formName.trim()) ||
                  selectedInputTypes.size === 0 ||
                  createMutation.isPending ||
                  updateMutation.isPending
                }
              >
                {(createMutation.isPending || updateMutation.isPending) && (
                  <Button.LeftIcon>
                    <Loader2 className="h-4 w-4 animate-spin" />
                  </Button.LeftIcon>
                )}
                <Button.Text>
                  {createMutation.isPending || updateMutation.isPending
                    ? "Saving..."
                    : editingPolicy
                      ? "Update"
                      : "Create"}
                </Button.Text>
              </Button>
            </SheetFooter>
          </SheetContent>
        </Sheet>

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
/*  PolicySheetBody                                                           */
/* -------------------------------------------------------------------------- */

function PolicySheetBody({
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
  selectedInputTypes,
  setSelectedInputTypes,
  formAction,
  setFormAction,
  formAutoName,
  setFormAutoName,
  formUserMessage,
  setFormUserMessage,
}: {
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
  selectedInputTypes: Set<PolicyInputType>;
  setSelectedInputTypes: (v: Set<PolicyInputType>) => void;
  formAction: PolicyAction;
  setFormAction: (v: PolicyAction) => void;
  formAutoName: boolean;
  setFormAutoName: (v: boolean) => void;
  formUserMessage: string;
  setFormUserMessage: (v: string) => void;
}) {
  const [expandedCategory, setExpandedCategory] = useState<
    RuleCategory | "custom" | null
  >(null);
  const [inputTypesOpen, setInputTypesOpen] = useState(
    () => selectedInputTypes.size !== ALL_POLICY_INPUT_TYPES.length,
  );
  const flagOnlySelected = [...FLAG_ONLY_CATEGORIES].some((c) =>
    selectedCategories.has(c),
  );
  const actionValue =
    flagOnlySelected && formAction === "block" ? "flag" : formAction;

  return (
    <div className="space-y-6 py-4">
      {/* Policy Name */}
      <div className="space-y-2">
        <div className="flex items-center justify-between">
          <Label className="text-sm font-medium">Policy Name</Label>
          <div className="flex items-center gap-2">
            <span className="text-muted-foreground text-xs">Auto</span>
            <Switch checked={formAutoName} onCheckedChange={setFormAutoName} />
          </div>
        </div>
        {formAutoName ? (
          <p className="text-muted-foreground text-xs">
            Name will be generated automatically based on detection rules and
            action.
          </p>
        ) : (
          <Input
            value={formName}
            onChange={(value) => setFormName(value)}
            placeholder="e.g. Secret Detection"
          />
        )}
      </div>

      {/* Detection Rules */}
      <div className="space-y-3">
        <Label className="text-sm font-medium">Detection Rules</Label>
        <div className="border-border divide-border divide-y rounded-lg border">
          {ALL_CATEGORIES.map((cat) => {
            const meta = RULE_CATEGORY_META[cat];
            const isAvailable = AVAILABLE_CATEGORIES.has(cat);
            const isExpanded = expandedCategory === cat;
            const rules = DETECTION_RULES[cat];
            const isExpandable = isAvailable && rules.length > 0;
            const categorySelected = selectedCategories.has(cat);
            const enabledRuleCount = categorySelected
              ? rules.filter((r) => !disabledRules.has(r.id)).length
              : 0;
            const hasPartialSelection =
              categorySelected &&
              rules.length > 0 &&
              enabledRuleCount > 0 &&
              enabledRuleCount < rules.length;
            const headerChecked: boolean | "indeterminate" = hasPartialSelection
              ? "indeterminate"
              : categorySelected &&
                (rules.length === 0 || enabledRuleCount > 0);

            const toggleCategory = (checked: boolean) => {
              const nextCats = new Set(selectedCategories);
              const nextDisabled = new Set(disabledRules);
              if (checked) {
                nextCats.add(cat);
                for (const rule of rules) nextDisabled.delete(rule.id);
              } else {
                nextCats.delete(cat);
                for (const rule of rules) nextDisabled.delete(rule.id);
              }
              setSelectedCategories(nextCats);
              setDisabledRules(nextDisabled);
              if (
                checked &&
                cat === "destructive_tool" &&
                formAction === "block"
              ) {
                setFormAction("flag");
              }
            };

            const toggleRule = (ruleId: string, enabled: boolean) => {
              const nextDisabled = new Set(disabledRules);
              const nextCats = new Set(selectedCategories);
              if (enabled) {
                nextDisabled.delete(ruleId);
                // Enabling any rule inside a category implies the category is
                // selected. Otherwise the rule wouldn't actually run.
                nextCats.add(cat);
              } else {
                nextDisabled.add(ruleId);
              }
              setSelectedCategories(nextCats);
              setDisabledRules(nextDisabled);
            };

            return (
              <div key={cat}>
                {/* Category header */}
                <div
                  className={cn(
                    "flex items-center gap-3 px-4 py-3",
                    isExpandable && "cursor-pointer",
                  )}
                  onClick={() => {
                    if (isExpandable) {
                      setExpandedCategory(isExpanded ? null : cat);
                    }
                  }}
                >
                  {/* Expand chevron (only for categories with rules to expand) */}
                  {isExpandable ? (
                    <ChevronRight
                      className={cn(
                        "text-muted-foreground h-4 w-4 shrink-0 transition-transform",
                        isExpanded && "rotate-90",
                      )}
                    />
                  ) : (
                    <div className="w-4 shrink-0" />
                  )}

                  {/* Category icon */}
                  <Icon
                    name={meta.icon as IconName}
                    className="text-muted-foreground size-4 shrink-0"
                  />

                  {/* Label & description */}
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2">
                      <span className="text-sm font-medium">{meta.label}</span>
                      {!isAvailable && (
                        <Badge variant="outline" className="text-[10px]">
                          Coming Soon
                        </Badge>
                      )}
                      {isExpandable && categorySelected && (
                        <Badge variant="outline" className="text-[10px]">
                          {enabledRuleCount}/{rules.length} on
                        </Badge>
                      )}
                    </div>
                    <p className="text-muted-foreground text-xs">
                      {meta.description}
                    </p>
                  </div>

                  {/* Category checkbox */}
                  <Checkbox
                    checked={headerChecked}
                    disabled={!isAvailable}
                    onCheckedChange={(checked) => toggleCategory(!!checked)}
                    onClick={(e) => e.stopPropagation()}
                  />
                </div>

                {/* Expanded per-rule toggles. Each rule is independently
                    toggleable; unchecking adds the canonical rule_id to the
                    policy's disabled_rules list and the scanner drops matching
                    findings. */}
                {isAvailable && isExpanded && rules.length > 0 && (
                  <div className="bg-muted/30 border-border border-t px-4 py-2">
                    <div className="flex items-center justify-between py-1">
                      <span className="text-muted-foreground text-xs">
                        {enabledRuleCount} of {rules.length} rules enabled
                      </span>
                      <div className="flex gap-3">
                        <button
                          type="button"
                          className="text-primary text-xs underline-offset-2 hover:underline disabled:opacity-50"
                          disabled={enabledRuleCount === rules.length}
                          onClick={() => {
                            const nextDisabled = new Set(disabledRules);
                            for (const r of rules) nextDisabled.delete(r.id);
                            setDisabledRules(nextDisabled);
                            const nextCats = new Set(selectedCategories);
                            nextCats.add(cat);
                            setSelectedCategories(nextCats);
                          }}
                        >
                          Enable all
                        </button>
                        <button
                          type="button"
                          className="text-primary text-xs underline-offset-2 hover:underline disabled:opacity-50"
                          disabled={!categorySelected || enabledRuleCount === 0}
                          onClick={() => {
                            const nextDisabled = new Set(disabledRules);
                            for (const r of rules) nextDisabled.add(r.id);
                            setDisabledRules(nextDisabled);
                          }}
                        >
                          Disable all
                        </button>
                      </div>
                    </div>
                    <div className="space-y-2 py-1">
                      {rules.map((rule) => {
                        const ruleEnabled =
                          categorySelected && !disabledRules.has(rule.id);
                        return (
                          <div
                            key={rule.id}
                            className="flex items-center gap-3 py-1 pl-8"
                          >
                            <Checkbox
                              id={rule.id}
                              checked={ruleEnabled}
                              onCheckedChange={(checked) =>
                                toggleRule(rule.id, !!checked)
                              }
                            />
                            <label htmlFor={rule.id} className="text-xs">
                              {rule.title}
                            </label>
                          </div>
                        );
                      })}
                    </div>
                  </div>
                )}
              </div>
            );
          })}
        </div>
      </div>

      {customRules.length > 0 && (
        <CustomRulesPicker
          customRules={customRules}
          selectedCustomRuleIds={selectedCustomRuleIds}
          setSelectedCustomRuleIds={setSelectedCustomRuleIds}
          expanded={expandedCategory === "custom"}
          onToggle={() =>
            setExpandedCategory(expandedCategory === "custom" ? null : "custom")
          }
        />
      )}

      <Collapsible
        open={inputTypesOpen}
        onOpenChange={setInputTypesOpen}
        className="space-y-3"
      >
        <div className="space-y-1">
          <Label className="text-sm font-medium">Message Types</Label>
          <p className="text-muted-foreground text-xs">
            Choose which parts of an agent session this policy scans. Leaving
            all four selected applies the policy everywhere.
          </p>
        </div>
        <div className="border-border rounded-lg border">
          <CollapsibleTrigger className="hover:bg-muted/40 flex w-full items-center gap-3 px-4 py-3 text-left transition-colors">
            <ChevronRight
              className={cn(
                "text-muted-foreground h-4 w-4 shrink-0 transition-transform",
                inputTypesOpen && "rotate-90",
              )}
            />
            <div className="min-w-0 flex-1">
              <div className="text-sm font-medium">
                {inputTypesSummary(selectedInputTypes)}
              </div>
              <div className="text-muted-foreground text-xs">
                Advanced: narrow scanning to specific parts of a session
              </div>
            </div>
          </CollapsibleTrigger>
          <CollapsibleContent className="border-border data-[state=closed]:animate-accordion-up data-[state=open]:animate-accordion-down overflow-hidden border-t">
            <div className="divide-border divide-y">
              {ALL_POLICY_INPUT_TYPES.map((type) => {
                const meta = POLICY_INPUT_TYPE_META[type];
                const checked = selectedInputTypes.has(type);

                return (
                  <label
                    key={type}
                    className="hover:bg-muted/40 flex cursor-pointer items-start gap-3 px-4 py-3"
                  >
                    <Checkbox
                      checked={checked}
                      onCheckedChange={(next) => {
                        const updated = new Set(selectedInputTypes);
                        if (next) {
                          updated.add(type);
                        } else {
                          updated.delete(type);
                        }
                        setSelectedInputTypes(updated);
                      }}
                    />
                    <div className="min-w-0 flex-1">
                      <div className="text-sm font-medium">{meta.label}</div>
                      <div className="text-muted-foreground text-xs">
                        {meta.description}
                      </div>
                    </div>
                  </label>
                );
              })}
            </div>
          </CollapsibleContent>
        </div>
        {selectedInputTypes.size === 0 && (
          <p className="text-destructive text-xs">
            Select at least one type. An empty API value means “all types,” so
            the UI keeps that choice explicit here.
          </p>
        )}
      </Collapsible>

      {/* Action */}
      <div className="space-y-2">
        <Label className="text-sm font-medium">Action</Label>
        <RadioGroup
          value={actionValue}
          onValueChange={(v) => {
            if (flagOnlySelected && v === "block") {
              return;
            }
            setFormAction(v as PolicyAction);
          }}
        >
          <div className="border-border divide-border divide-y rounded-lg border">
            {ACTION_OPTIONS.map((opt) => {
              const disabled = flagOnlySelected && opt.value === "block";

              return (
                <label
                  key={opt.value}
                  htmlFor={`action-${opt.value}`}
                  className={cn(
                    "flex items-start gap-3 p-3",
                    disabled
                      ? "cursor-not-allowed opacity-60"
                      : "hover:bg-muted/50 cursor-pointer",
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
                    </div>
                    <div className="text-muted-foreground mt-1 text-xs">
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
          </div>
        </RadioGroup>
      </div>

      {/* Custom message — only relevant for block-action policies that
          surface a user-facing reason at deny time. Flag-action policies
          record findings silently, so no message is needed. */}
      {formAction === "block" && (
        <div className="space-y-2">
          <Label className="text-sm font-medium">Custom Message</Label>
          <p className="text-muted-foreground text-xs">
            Shown to the user when this policy blocks a tool call or prompt.
            Leave blank to use the default message.
          </p>
          <TextArea
            value={formUserMessage}
            onChange={setFormUserMessage}
            placeholder="e.g. This action was blocked by your organization's security policy. Contact your admin for help."
            rows={3}
          />
        </div>
      )}

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
                    onClick={() => refetch()}
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
  { label: string; variant: "secondary" | "destructive" }
> = {
  flag: { label: "Flag", variant: "secondary" },
  block: { label: "Block", variant: "destructive" },
};

const ACTION_OPTIONS: { value: PolicyAction; description: string }[] = [
  {
    value: "flag",
    description: "Log findings for review without interrupting the session",
  },
  {
    value: "block",
    description: "Deny prompts and tool calls that match detection rules",
  },
];

function ActionBadge({ action }: { action: PolicyAction }) {
  const config = ACTION_BADGE_CONFIG[action] ?? ACTION_BADGE_CONFIG.flag;
  return <Badge variant={config.variant}>{config.label}</Badge>;
}

/* -------------------------------------------------------------------------- */
/*  CustomRulesPicker                                                          */
/* -------------------------------------------------------------------------- */

function CustomRulesPicker({
  customRules,
  selectedCustomRuleIds,
  setSelectedCustomRuleIds,
  expanded,
  onToggle,
}: {
  customRules: ReturnType<typeof useDetectionRulesStore>["customRules"];
  selectedCustomRuleIds: Set<string>;
  setSelectedCustomRuleIds: (v: Set<string>) => void;
  expanded: boolean;
  onToggle: () => void;
}) {
  const meta = RULE_CATEGORY_META.custom;
  const allSelected =
    customRules.length > 0 &&
    customRules.every((r) => selectedCustomRuleIds.has(r.id));
  const someSelected =
    !allSelected && customRules.some((r) => selectedCustomRuleIds.has(r.id));
  return (
    <div className="space-y-3">
      <Label className="text-sm font-medium">Custom Rules</Label>
      <div className="border-border divide-border divide-y rounded-lg border">
        <div
          className="flex cursor-pointer items-center gap-3 px-4 py-3"
          onClick={onToggle}
        >
          <ChevronRight
            className={cn(
              "text-muted-foreground h-4 w-4 shrink-0 transition-transform",
              expanded && "rotate-90",
            )}
          />
          <Icon
            name={meta.icon as IconName}
            className="text-muted-foreground size-4 shrink-0"
          />
          <div className="min-w-0 flex-1">
            <span className="text-sm font-medium">{meta.label}</span>
            <p className="text-muted-foreground text-xs">
              {customRules.length} organization-defined rule
              {customRules.length === 1 ? "" : "s"}
            </p>
          </div>
          <Checkbox
            checked={
              allSelected ? true : someSelected ? "indeterminate" : false
            }
            onCheckedChange={(checked) => {
              const next = new Set(selectedCustomRuleIds);
              if (checked) {
                customRules.forEach((r) => next.add(r.id));
              } else {
                customRules.forEach((r) => next.delete(r.id));
              }
              setSelectedCustomRuleIds(next);
            }}
            onClick={(e) => e.stopPropagation()}
          />
        </div>
        {expanded && (
          <div className="bg-muted/30 border-border border-t px-4 py-2">
            <div className="space-y-2 py-1">
              {customRules.map((rule) => {
                const checked = selectedCustomRuleIds.has(rule.id);
                return (
                  <div
                    key={rule.id}
                    className="flex items-center gap-3 py-1 pl-8"
                  >
                    <Checkbox
                      id={`custom-${rule.id}`}
                      checked={checked}
                      onCheckedChange={(next) => {
                        const set = new Set(selectedCustomRuleIds);
                        if (next) {
                          set.add(rule.id);
                        } else {
                          set.delete(rule.id);
                        }
                        setSelectedCustomRuleIds(set);
                      }}
                    />
                    <label
                      htmlFor={`custom-${rule.id}`}
                      className="cursor-pointer text-xs"
                    >
                      <span className="text-foreground">
                        {rule.title || rule.id}
                      </span>
                      <span className="text-muted-foreground ml-2 font-mono text-[10px]">
                        {rule.id}
                      </span>
                    </label>
                  </div>
                );
              })}
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
