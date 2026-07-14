import { InsightsConfig } from "@/components/insights-dock";
import { INSIGHTS_SUGGESTIONS } from "@/lib/insights-suggestions";
import { ListLayout } from "@/components/layouts/list-layout";
import { Page } from "@/components/page-layout";
import { ReleaseStageBadge } from "@/components/release-stage-badge";
import { RequireScope } from "@/components/require-scope";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Checkbox } from "@/components/ui/checkbox";
import { Label } from "@/components/ui/label";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { SearchBar } from "@/components/ui/search-bar";
import { InlineEmptyState } from "@/components/ui/inline-empty-state";
import { Progress } from "@/components/ui/progress";
import { StatusDot, type StatusDotTone } from "@/components/ui/status-dot";
import { Switch } from "@/components/ui/switch";
import { Dialog } from "@/components/ui/dialog";
import { SimpleTooltip } from "@/components/ui/tooltip";
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
import { Stack } from "@/components/ui/stack";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { type Column, Table } from "@/components/ui/table";
import { Input } from "@/components/ui/input";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { DynamicIcon, type IconName } from "@/components/ui/dynamic-icon";
import { type BadgeProps } from "@/components/ui/badge";
import {
  Plus,
  Shield,
  Ellipsis,
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
import { useMembers } from "@gram/client/react-query/members.js";
import { useRiskCreatePolicyMutation } from "@gram/client/react-query/riskCreatePolicy.js";
import {
  invalidateAllRiskListPolicies,
  useRiskListPolicies,
} from "@gram/client/react-query/riskListPolicies.js";
import { useRiskPoliciesDeleteMutation } from "@gram/client/react-query/riskPoliciesDelete.js";
import { useRoles } from "@gram/client/react-query/roles.js";
import {
  useRiskPoliciesStatus,
  invalidateAllRiskPoliciesStatus,
} from "@gram/client/react-query/riskPoliciesStatus.js";
import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
import type { WorkflowStatus } from "@gram/client/models/components/riskpolicystatus.js";
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
import { useDetectionRulesStore } from "./detection-rules-data";
import { useTelemetry } from "@/contexts/Telemetry";
import { useRoutes } from "@/routes";
import { Outlet } from "react-router";
import {
  ACTION_OPTIONS,
  ALL_POLICY_MESSAGE_TYPES,
  AVAILABLE_CATEGORIES,
  categoriesToPayload,
  policyMessageTypesForForm,
  policyToCategories,
} from "./policy-form";
import {
  getPolicyDeleteImpactText,
  getPolicyDeleteRuleListItems,
  getPolicyRuleGroupNamesForDeleteDialog,
} from "./policy-delete-dialog";

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
  return (
    <div
      className={cn(
        "flex gap-3 border p-3 transition-colors",
        selected ? "border-foreground bg-muted/40" : "border-border",
      )}
    >
      <DynamicIcon
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

/** Per-policy config for the Non-Corporate Accounts category: the list of
 *  email domains treated as corporate. Rendered inside the category's
 *  Customize sheet; the parsed list rides on the create/update payload as
 *  approved_email_domains while the category is selected. */
function ApprovedDomainsConfig({
  value,
  onChange,
}: {
  value: string;
  onChange: (v: string) => void;
}) {
  return (
    <div className="space-y-2">
      <Label className="text-sm font-medium">Approved email domains</Label>
      <p className="text-muted-foreground text-xs">
        Sessions from AI accounts whose email domain is not in this list are
        flagged by the unapproved-domain rule. Matching is exact per domain, so
        list subdomains explicitly; a leading '@' is allowed. Until at least one
        domain is configured, only the personal-account rule fires.
      </p>
      <Input
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder="e.g. acme.com, corp.acme.com"
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
  approvedDomains,
  setApprovedDomains,
  onClose,
}: {
  category: RuleCategory;
  selectedCategories: Set<RuleCategory>;
  setSelectedCategories: (v: Set<RuleCategory>) => void;
  disabledRules: Set<string>;
  setDisabledRules: (v: Set<string>) => void;
  approvedDomains: string;
  setApprovedDomains: (v: string) => void;
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
        {category === "account_identity" && (
          <div className="border-border mx-6 mt-3 border-b pb-4">
            <ApprovedDomainsConfig
              value={approvedDomains}
              onChange={setApprovedDomains}
            />
          </div>
        )}
        <div className="px-6 pt-3">
          <Input
            value={search}
            onChange={(e) => setSearch(e.target.value)}
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
    <div className="hover:bg-muted flex items-center justify-between gap-3 px-2 py-2 text-sm">
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

/** Map sources to display categories for the table row badges. */
function sourcesToCategories(
  sources: string[],
  presidioEntities?: string[],
): RuleCategory[] {
  return [...policyToCategories(sources, presidioEntities)];
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

function isPromptPolicy(policy: RiskPolicy): boolean {
  return policy.policyType === "prompt_based";
}

export default function PolicyCenter(): JSX.Element {
  return (
    <RequireScope scope="org:admin" level="page">
      <PolicyCenterContent />
    </RequireScope>
  );
}

// Layout route wrapper: the policy list and the policy detail subpage render
// through this Outlet (see routes.tsx policyCenter).
export function PolicyCenterRoot(): JSX.Element {
  return <Outlet />;
}

function PolicyCenterContent() {
  const queryClient = useQueryClient();
  const routes = useRoutes();
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

  const [runPanelPolicy, setRunPanelPolicy] = useState<RiskPolicy | null>(null);
  const [policyToDelete, setPolicyToDelete] = useState<PolicyRow | null>(null);

  const [activeTab, setActiveTab] = useState<"policies" | "exclusions">(
    "policies",
  );
  const [exclusionSheet, setExclusionSheet] =
    useState<ExclusionSheetState | null>(null);

  // Deep-link support: `?policy=<id>` redirects to that policy's detail page.
  // The command palette uses this since policies have no per-item list route.
  const [policyParam] = useQueryState("policy");
  const openedPolicyRef = useRef<string | null>(null);

  const invalidate = useCallback(() => {
    void invalidateAllRiskListPolicies(queryClient);
    void invalidateAllRiskPoliciesStatus(queryClient);
  }, [queryClient]);

  const createMutation = useRiskCreatePolicyMutation({
    onSuccess: invalidate,
  });

  const deleteMutation = useRiskPoliciesDeleteMutation({
    onSuccess: () => {
      setPolicyToDelete(null);
      invalidate();
    },
  });

  // Redirect a deep-linked policy to its detail page once its data has loaded.
  // Guarded by a ref so it fires once per id (not on every policies re-fetch),
  // and marks unknown ids handled so a stale id doesn't retry every render.
  useEffect(() => {
    if (!policyParam || isLoading) return;
    if (openedPolicyRef.current === policyParam) return;
    const policy = data?.policies?.find((p) => p.id === policyParam);
    openedPolicyRef.current = policyParam;
    if (policy) {
      routes.policyCenter.detail.goTo(policyParam);
    }
  }, [policyParam, isLoading, data, routes]);

  const handleDelete = (row: PolicyRow) => {
    setPolicyToDelete(row);
  };

  const confirmDelete = () => {
    if (!policyToDelete) return;
    deleteMutation.mutate({ request: { id: policyToDelete.policy.id } });
  };

  // Empty state for the Policies tab only. It must NOT short-circuit the whole
  // page, otherwise the Exclusions tab (and global exclusions) would be
  // unreachable for projects that have no policies yet.
  const policiesEmptyState = (
    <InlineEmptyState
      icon={<Shield />}
      title="No Risk Policies"
      description="Risk policies scan your chat messages for secrets and sensitive data. Create your first policy to get started."
      action={
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
          <Button.Text className="flex gap-2">
            {createMutation.isPending ? (
              <>
                <Loader2 className="h-4 w-4 animate-spin" />
                Creating...
              </>
            ) : (
              "Get Started"
            )}
          </Button.Text>
        </Button>
      }
    />
  );

  const insightsContext = [
    "Page: Policy Center.",
    `Total policies: ${policyRows.length}.`,
    `Policy actions: ${policyRows.map((r) => `${r.policy.name} (${r.policy.action})`).join(", ") || "none"}.`,
    "Available risk tools: listRiskPolicies, getRiskPolicy, getRiskPolicyStatus, listRiskResultsForAgent (finding-level with match redaction), listRiskResultsByChat, listShadowMCPApprovals.",
    "Never echo match_redacted values verbatim. Refer to findings by rule_id and source.",
  ].join(" ");

  const policyColumns: Column<PolicyRow>[] = [
    {
      key: "name",
      header: "Name",
      width: "1fr",
      render: (row) => (
        <span className="flex min-w-0 items-center gap-1.5 font-medium">
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
        <span className="inline-flex">
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
              <span className="text-muted-foreground block max-w-full truncate text-sm italic">
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
          <span className="text-muted-foreground text-sm">
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
              <span className="text-muted-foreground text-sm">
                {messageTypesSummary(typeSet)}
              </span>
            </SimpleTooltip>
          );
        }

        return (
          <span className="text-muted-foreground text-sm">
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
        <span className="text-muted-foreground text-sm">
          {policyAudienceSummary(row)}
        </span>
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
                  // Both prompt and standard policies now edit on their
                  // dedicated detail page.
                  routes.policyCenter.detail.goTo(row.policy.id);
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
                onSelect={() => {
                  setTimeout(() => handleDelete(row), 0);
                }}
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
      ? { label: "New Policy", onClick: () => routes.policyCenter.new.goTo() }
      : {
          label: "Create Exclusion",
          onClick: () => setExclusionSheet({ mode: "create" }),
        };
  const policyDeleteRuleListItems = policyToDelete
    ? getPolicyDeleteRuleListItems(
        getPolicyRuleGroupNamesForDeleteDialog(policyToDelete.policy),
      )
    : [];
  const policyDeleteImpactText = policyToDelete
    ? getPolicyDeleteImpactText(
        policyToDelete.policy,
        policyDeleteRuleListItems.length > 0,
      )
    : "";

  let policiesBody = (
    <Table
      columns={policyColumns}
      data={policyRows}
      rowKey={(row) => row.policy.id}
      onRowClick={(row) =>
        // Both prompt and standard policies open their dedicated detail page
        // (eval workbench for prompt, on-page editor for standard).
        routes.policyCenter.detail.goTo(row.policy.id)
      }
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
    <Button onClick={headerAction.onClick}>
      <Button.LeftIcon>
        <Plus className="mr-2 h-4 w-4" />
      </Button.LeftIcon>
      <Button.Text>{headerAction.label}</Button.Text>
    </Button>
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
        <ListLayout>
          <ListLayout.Header
            title={
              <span className="inline-flex items-center gap-2">
                Policies
                <ReleaseStageBadge stage="beta" />
              </span>
            }
            subtitle="Configure policies to detect secrets, sensitive information, and prompt-defined risks in agent session interactions."
            actions={cta}
          />
          <ListLayout.List>
            <Tabs
              value={activeTab}
              onValueChange={(value) =>
                setActiveTab(value as "policies" | "exclusions")
              }
            >
              <div className="border-b">
                <TabsList className="h-auto justify-start gap-4 bg-transparent p-0 text-sm">
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
          </ListLayout.List>
        </ListLayout>

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

        {/* Delete Policy Confirmation */}
        <Dialog
          open={!!policyToDelete}
          onOpenChange={(open) => {
            if (!open) setPolicyToDelete(null);
          }}
        >
          <Dialog.Content>
            <Dialog.Header>
              <Dialog.Title>Delete Policy</Dialog.Title>
            </Dialog.Header>
            <Stack gap={4}>
              <Type variant="body">
                <code className="bg-muted px-1 py-0.5 font-mono font-bold">
                  {policyToDelete?.policy.name}
                </code>{" "}
                policy will be permanently deleted.
              </Type>
              {policyDeleteImpactText && (
                <Type variant="body">{policyDeleteImpactText}</Type>
              )}
              {policyDeleteRuleListItems.length > 0 && (
                <div className="space-y-2">
                  <ul className="list-disc space-y-1 pl-5">
                    {policyDeleteRuleListItems.map((ruleName, index) => (
                      <li key={`${ruleName}-${index}`}>
                        <Type variant="body" muted as="span">
                          {ruleName}
                        </Type>
                      </li>
                    ))}
                  </ul>
                </div>
              )}
            </Stack>
            <Dialog.Footer>
              <div className="flex justify-end gap-2">
                <Button
                  variant="secondary"
                  onClick={() => setPolicyToDelete(null)}
                >
                  Cancel
                </Button>
                <Button
                  variant="destructive-primary"
                  onClick={confirmDelete}
                  disabled={deleteMutation.isPending}
                >
                  Delete Policy
                </Button>
              </div>
            </Dialog.Footer>
          </Dialog.Content>
        </Dialog>
      </Page.Body>
    </Page>
  );
}

export function PolicyAudiencePicker({
  formAudienceType,
  setFormAudienceType,
  selectedAudiencePrincipalUrns,
  setSelectedAudiencePrincipalUrns,
}: {
  formAudienceType: PolicyAudienceType;
  setFormAudienceType: (v: PolicyAudienceType) => void;
  selectedAudiencePrincipalUrns: Set<string>;
  setSelectedAudiencePrincipalUrns: (v: Set<string>) => void;
}): JSX.Element {
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
        <div className="border-border divide-border divide-y border">
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
        <div className="border-border border">
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
    <div className="border-border border">
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
            <div className="border-border divide-border divide-y overflow-hidden border">
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
      <div className="border-border divide-border divide-y overflow-hidden border">
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

const WORKFLOW_STATUS_PRESENTATION: Record<
  WorkflowStatus,
  { tone: StatusDotTone; pulse: boolean }
> = {
  running: { tone: "success", pulse: true },
  sleeping: { tone: "warning", pulse: false },
  not_started: { tone: "neutral", pulse: false },
};

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
              <div className="border-border border p-3">
                <p className="text-muted-foreground mb-1 font-mono text-xs tracking-[0.08em] uppercase">
                  Status
                </p>
                <StatusDot
                  {...WORKFLOW_STATUS_PRESENTATION[status.workflowStatus]}
                  label={
                    <span className="font-medium capitalize">
                      {status.workflowStatus === "not_started"
                        ? "Idle"
                        : status.workflowStatus}
                    </span>
                  }
                />
              </div>
              <div className="border-border border p-3">
                <p className="text-muted-foreground mb-1 font-mono text-xs tracking-[0.08em] uppercase">
                  Version
                </p>
                <p className="text-sm font-medium">v{status.policyVersion}</p>
              </div>
            </div>

            {/* Progress */}
            <div className="border-border border p-4">
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
              <Progress value={pct} className="mb-2" />
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
            <div className="border-border border p-4">
              <p className="text-muted-foreground mb-1 font-mono text-xs tracking-[0.08em] uppercase">
                Findings
              </p>
              <p className="font-display text-3xl font-thin tracking-[-0.02em] tabular-nums">
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

function ActionBadge({ action }: { action: PolicyAction }): JSX.Element {
  const config = ACTION_BADGE_CONFIG[action] ?? ACTION_BADGE_CONFIG.flag;
  return (
    <Badge variant={config.variant}>
      <Badge.Text>{config.label}</Badge.Text>
    </Badge>
  );
}

/** One session-part as a selectable card (Scope step). */
export function ScopeCard({
  type,
  checked,
  onToggle,
}: {
  type: PolicyMessageType;
  checked: boolean;
  onToggle: (checked: boolean) => void;
}): JSX.Element {
  const meta = POLICY_MESSAGE_TYPE_META[type];
  return (
    <label
      className={cn(
        "flex cursor-pointer items-start gap-3 border p-3 transition-colors",
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

export function ActionPicker({
  formAction,
  setFormAction,
  flagOnlySelected = false,
}: {
  formAction: PolicyAction;
  setFormAction: (v: PolicyAction) => void;
  flagOnlySelected?: boolean;
}): JSX.Element {
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
              "flex items-start gap-3 border p-3.5 transition-colors",
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
export function RuleSelectList({
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
}): JSX.Element {
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
        <div className="border-border divide-border divide-y border">
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
