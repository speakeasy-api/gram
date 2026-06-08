import { useEffect, useMemo, useState } from "react";
import { Link as RouterLink } from "react-router";
import { useQueryClient } from "@tanstack/react-query";
import {
  invalidateAllRiskListPolicies,
  useRiskCreatePolicyMutation,
  useRiskListPolicies,
  useRiskPoliciesDeleteMutation,
} from "@gram/client/react-query/index.js";
import { invalidateAllRiskPoliciesStatus } from "@gram/client/react-query/riskPoliciesStatus.js";
import {
  ShieldCheck,
  ShieldOff,
  KeyRound,
  User,
  CreditCard,
  Landmark,
  HeartPulse,
  Syringe,
  ChevronRight,
  ExternalLink,
  Lock,
  type LucideIcon,
} from "lucide-react";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { Switch } from "@/components/ui/switch";
import { Checkbox } from "@/components/ui/checkbox";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { Label } from "@/components/ui/label";
import { Badge, Button, Link } from "@speakeasy-api/moonshine";
import { useSlugs } from "@/contexts/Sdk";
import { StepContainer } from "../step-container";
import {
  RULE_CATEGORY_META,
  DETECTION_RULES,
  POLICY_MESSAGE_TYPE_META,
  type RuleCategory,
  type PolicyAction,
  type PolicyMessageType,
} from "@/pages/security/policy-data";
import { cn } from "@/lib/utils";

interface ConfigurePoliciesStepProps {
  onComplete: () => void;
  onBack: () => void;
}

const CATEGORY_ICONS: Partial<Record<RuleCategory, LucideIcon>> = {
  secrets: KeyRound,
  pii: User,
  prompt_injection: Syringe,
  financial: CreditCard,
  government_ids: Landmark,
  healthcare: HeartPulse,
  shadow_mcp: ShieldOff,
};

type CategoryConfig = {
  enabled: boolean;
  action: PolicyAction;
  messageTypes: Set<PolicyMessageType>;
};

// Onboarding-scoped category list. Shadow MCP is pinned as a dedicated hero
// row (different mental model — it gates *which* MCP servers can be called,
// not what data flows through them), so it lives outside this array.
const WIZARD_CATEGORIES: RuleCategory[] = [
  "secrets",
  "pii",
  "prompt_injection",
  "financial",
  "government_ids",
  "healthcare",
];

// Smart defaults per category. Mirrors what we'd seed in the Policy Center if
// the user just clicked through onboarding accepting everything.
const DEFAULTS: Record<RuleCategory, CategoryConfig> = {
  secrets: {
    enabled: true,
    action: "block",
    messageTypes: new Set(["tool_request", "tool_response"]),
  },
  pii: {
    enabled: true,
    action: "flag",
    messageTypes: new Set(["tool_request", "tool_response"]),
  },
  prompt_injection: {
    enabled: true,
    action: "flag",
    messageTypes: new Set(["tool_response"]),
  },
  financial: {
    enabled: false,
    action: "flag",
    messageTypes: new Set(["tool_request", "tool_response"]),
  },
  government_ids: {
    enabled: false,
    action: "flag",
    messageTypes: new Set(["tool_request", "tool_response"]),
  },
  healthcare: {
    enabled: false,
    action: "flag",
    messageTypes: new Set(["tool_request", "tool_response"]),
  },
  shadow_mcp: {
    enabled: false,
    action: "block",
    messageTypes: new Set(["tool_request"]),
  },
  // Categories not shown in the wizard still need a default so type checking
  // and shared helpers can stay polymorphic; they're inert at this layer.
  off_policy: {
    enabled: false,
    action: "flag",
    messageTypes: new Set(["user_message"]),
  },
  destructive_tool: {
    enabled: false,
    action: "flag",
    messageTypes: new Set(["tool_request"]),
  },
  cli_destructive: {
    enabled: false,
    action: "flag",
    messageTypes: new Set(["tool_request"]),
  },
  custom: {
    enabled: false,
    action: "flag",
    messageTypes: new Set(["tool_request", "tool_response"]),
  },
};

const MESSAGE_TYPES: PolicyMessageType[] = [
  "user_message",
  "tool_request",
  "tool_response",
  "assistant_message",
];

// Categories with too many underlying rules to manage in a wizard list. Drawer
// shows a "customize in Policy Center" handoff instead of a rule picker.
const DEFER_RULES_TO_POLICY_CENTER: Partial<Record<RuleCategory, boolean>> = {
  secrets: true,
  prompt_injection: true,
  pii: true,
};

function formatMessageTypes(types: Set<PolicyMessageType>): string {
  if (types.size === 0) return "Off — no message types";
  if (types.size === MESSAGE_TYPES.length) return "All message types";
  return Array.from(types)
    .map((t) => POLICY_MESSAGE_TYPE_META[t].label)
    .join(", ");
}

export function ConfigurePoliciesStep({
  onComplete,
  onBack,
}: ConfigurePoliciesStepProps) {
  const { orgSlug = "" } = useSlugs();
  const [configs, setConfigs] = useState<Record<RuleCategory, CategoryConfig>>(
    () => {
      const next = {} as Record<RuleCategory, CategoryConfig>;
      (Object.keys(DEFAULTS) as RuleCategory[]).forEach((k) => {
        next[k] = {
          ...DEFAULTS[k],
          messageTypes: new Set(DEFAULTS[k].messageTypes),
        };
      });
      return next;
    },
  );
  const [openCategory, setOpenCategory] = useState<RuleCategory | null>(null);

  const queryClient = useQueryClient();
  const { data: policiesData } = useRiskListPolicies();
  const shadowPolicy = useMemo(
    () =>
      (policiesData?.policies ?? []).find((p) =>
        (p.sources ?? []).includes("shadow_mcp"),
      ),
    [policiesData],
  );

  const invalidatePolicies = () => {
    invalidateAllRiskListPolicies(queryClient);
    invalidateAllRiskPoliciesStatus(queryClient);
  };

  const createShadowMutation = useRiskCreatePolicyMutation({
    onSuccess: invalidatePolicies,
    onError: () => {
      setConfigs((prev) => ({
        ...prev,
        shadow_mcp: { ...prev.shadow_mcp, enabled: false },
      }));
    },
  });
  const deleteShadowMutation = useRiskPoliciesDeleteMutation({
    onSuccess: invalidatePolicies,
    onError: () => {
      setConfigs((prev) => ({
        ...prev,
        shadow_mcp: { ...prev.shadow_mcp, enabled: true },
      }));
    },
  });

  useEffect(() => {
    setConfigs((prev) => {
      const desired = !!shadowPolicy;
      if (prev.shadow_mcp.enabled === desired) return prev;
      return {
        ...prev,
        shadow_mcp: { ...prev.shadow_mcp, enabled: desired },
      };
    });
  }, [shadowPolicy]);

  const handleShadowToggle = (checked: boolean) => {
    setConfigs((prev) => ({
      ...prev,
      shadow_mcp: { ...prev.shadow_mcp, enabled: checked },
    }));
    if (checked) {
      if (shadowPolicy) return;
      createShadowMutation.mutate({
        request: {
          createRiskPolicyRequestBody: {
            enabled: true,
            sources: ["shadow_mcp"],
            messageTypes: ["tool_request"],
            action: "block",
            autoName: true,
          },
        },
      });
    } else {
      if (!shadowPolicy) return;
      deleteShadowMutation.mutate({ request: { id: shadowPolicy.id } });
    }
  };

  const policyCenterHref = useMemo(
    () => (orgSlug ? `/${orgSlug}/risk-policies` : "/risk-policies"),
    [orgSlug],
  );

  const updateConfig = (cat: RuleCategory, patch: Partial<CategoryConfig>) => {
    setConfigs((prev) => ({
      ...prev,
      [cat]: { ...prev[cat], ...patch },
    }));
  };

  const toggleMessageType = (cat: RuleCategory, t: PolicyMessageType) => {
    setConfigs((prev) => {
      const next = new Set(prev[cat].messageTypes);
      if (next.has(t)) next.delete(t);
      else next.add(t);
      return { ...prev, [cat]: { ...prev[cat], messageTypes: next } };
    });
  };

  const shadow = configs.shadow_mcp;
  const enabledCount = WIZARD_CATEGORIES.filter(
    (c) => configs[c].enabled,
  ).length;

  const activeCategory = openCategory;
  const activeMeta = activeCategory ? RULE_CATEGORY_META[activeCategory] : null;
  const activeConfig = activeCategory ? configs[activeCategory] : null;
  const ActiveIcon = activeCategory
    ? (CATEGORY_ICONS[activeCategory] ?? ShieldCheck)
    : ShieldCheck;
  const isShadowMcp = activeCategory === "shadow_mcp";
  const activeRuleCount = activeCategory
    ? (DETECTION_RULES[activeCategory]?.length ?? 0)
    : 0;
  const deferRules =
    activeCategory && DEFER_RULES_TO_POLICY_CENTER[activeCategory];

  return (
    <StepContainer
      icon={
        <div className="bg-secondary flex h-12 w-12 items-center justify-center rounded-lg">
          <ShieldCheck className="text-foreground h-6 w-6" />
        </div>
      }
      title="Configure policies"
      description="Pick what Speakeasy should flag or block in agent traffic. You can refine actions, message scopes, and individual rules any time in the Policy Center."
      onContinue={onComplete}
      continueLabel="Complete setup"
      showBack
      onBack={onBack}
    >
      <div className="space-y-12">
        <ShadowMcpHero config={shadow} onToggle={handleShadowToggle} />

        <div className="space-y-2">
          <div className="flex items-baseline justify-between px-1">
            <div className="flex items-baseline gap-2">
              <p className="text-muted-foreground text-[11px] font-semibold tracking-[0.08em] uppercase">
                Detection categories
              </p>
              <span className="text-muted-foreground/70 text-[11px] tabular-nums">
                {enabledCount}/{WIZARD_CATEGORIES.length} enabled
              </span>
            </div>
            <RouterLink
              to={policyCenterHref}
              target="_blank"
              rel="noopener noreferrer"
              className="text-muted-foreground hover:text-foreground inline-flex items-center gap-1 text-[11px] font-medium transition-colors"
            >
              Open Policy Center
              <ExternalLink className="h-3 w-3" />
            </RouterLink>
          </div>

          <div className="border-border bg-card divide-border/60 divide-y overflow-hidden rounded-xl border">
            {WIZARD_CATEGORIES.map((cat) => {
              const meta = RULE_CATEGORY_META[cat];
              const cfg = configs[cat];
              const Icon = CATEGORY_ICONS[cat] ?? ShieldCheck;
              return (
                <button
                  key={cat}
                  type="button"
                  onClick={() => setOpenCategory(cat)}
                  className="group hover:bg-secondary/40 flex w-full items-center gap-4 px-4 py-3.5 text-left transition-colors"
                >
                  <div
                    className={cn(
                      "flex h-9 w-9 flex-shrink-0 items-center justify-center rounded-lg transition-colors",
                      cfg.enabled
                        ? "bg-foreground/8 text-foreground"
                        : "bg-secondary text-muted-foreground/70",
                    )}
                  >
                    <Icon className="h-[18px] w-[18px]" strokeWidth={1.75} />
                  </div>
                  <div className="min-w-0 flex-1">
                    <p
                      className={cn(
                        "truncate text-sm font-medium leading-tight",
                        cfg.enabled
                          ? "text-foreground"
                          : "text-muted-foreground",
                      )}
                    >
                      {meta.label}
                    </p>
                    <p className="text-muted-foreground mt-1 truncate text-xs">
                      {cfg.enabled
                        ? formatMessageTypes(cfg.messageTypes)
                        : "Disabled"}
                    </p>
                  </div>
                  <ActionPill action={cfg.enabled ? cfg.action : "off"} />
                  <ChevronRight
                    className="text-muted-foreground/50 group-hover:text-muted-foreground h-4 w-4 flex-shrink-0 transition-colors"
                    strokeWidth={2}
                  />
                </button>
              );
            })}
          </div>
        </div>
      </div>

      <Sheet
        open={!!openCategory}
        onOpenChange={(open) => {
          if (!open) setOpenCategory(null);
        }}
      >
        <SheetContent
          side="right"
          className="flex w-full flex-col overflow-hidden sm:max-w-[662px]"
        >
          {activeCategory && activeMeta && activeConfig && (
            <>
              <SheetHeader className="sr-only">
                <SheetTitle>Configure {activeMeta.label}</SheetTitle>
                <SheetDescription>{activeMeta.description}</SheetDescription>
              </SheetHeader>

              <div className="flex items-start gap-4 px-6 pt-6 pr-14">
                <div className="bg-secondary flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-md">
                  <ActiveIcon className="text-foreground h-5 w-5" />
                </div>
                <div className="min-w-0 flex-1">
                  <h4 className="text-foreground text-base font-medium">
                    {activeMeta.label}
                  </h4>
                  <p className="text-muted-foreground mt-1 text-sm leading-relaxed">
                    {activeMeta.description}
                  </p>
                </div>
              </div>

              <div className="flex-1 space-y-6 overflow-y-auto px-6 py-6">
                <section className="space-y-3">
                  <div className="flex items-center justify-between">
                    <p className="text-muted-foreground text-[11px] font-medium tracking-wider uppercase">
                      Detection
                    </p>
                    <div className="flex items-center gap-2">
                      <span className="text-muted-foreground text-xs">
                        {activeConfig.enabled ? "Enabled" : "Disabled"}
                      </span>
                      <Switch
                        checked={activeConfig.enabled}
                        onCheckedChange={(checked) =>
                          updateConfig(activeCategory, { enabled: checked })
                        }
                        aria-label="Enable detection"
                      />
                    </div>
                  </div>
                </section>

                <section className="space-y-3">
                  <div className="flex items-center gap-2">
                    <p className="text-muted-foreground text-[11px] font-medium tracking-wider uppercase">
                      Action
                    </p>
                    {isShadowMcp && (
                      <span className="text-muted-foreground inline-flex items-center gap-1 text-[11px]">
                        <Lock className="h-3 w-3" />
                        Locked to Block
                      </span>
                    )}
                  </div>
                  <RadioGroup
                    value={activeConfig.action}
                    onValueChange={(v) =>
                      updateConfig(activeCategory, {
                        action: v as PolicyAction,
                      })
                    }
                    disabled={!activeConfig.enabled || isShadowMcp}
                    className="grid grid-cols-2 gap-2"
                  >
                    <ActionRadio
                      value="flag"
                      label="Flag"
                      description="Record a finding, let the call through"
                      disabled={!activeConfig.enabled || isShadowMcp}
                      selected={activeConfig.action === "flag"}
                    />
                    <ActionRadio
                      value="block"
                      label="Block"
                      description="Reject the call, return an error"
                      disabled={!activeConfig.enabled || isShadowMcp}
                      selected={activeConfig.action === "block"}
                    />
                  </RadioGroup>
                </section>

                <section className="space-y-3">
                  <p className="text-muted-foreground text-[11px] font-medium tracking-wider uppercase">
                    Apply to
                  </p>
                  <div className="space-y-2">
                    {MESSAGE_TYPES.map((t) => {
                      const meta = POLICY_MESSAGE_TYPE_META[t];
                      const checked = activeConfig.messageTypes.has(t);
                      const id = `msg-${activeCategory}-${t}`;
                      return (
                        <label
                          key={t}
                          htmlFor={id}
                          className={cn(
                            "border-border bg-secondary/20 flex items-start gap-3 rounded-md border p-3",
                            !activeConfig.enabled && "opacity-50",
                          )}
                        >
                          <Checkbox
                            id={id}
                            checked={checked}
                            disabled={!activeConfig.enabled}
                            onCheckedChange={() =>
                              toggleMessageType(activeCategory, t)
                            }
                          />
                          <div className="min-w-0 flex-1">
                            <Label htmlFor={id} className="cursor-pointer">
                              {meta.label}
                            </Label>
                            <p className="text-muted-foreground mt-0.5 text-xs leading-relaxed">
                              {meta.description}
                            </p>
                          </div>
                        </label>
                      );
                    })}
                  </div>
                </section>

                {!isShadowMcp && activeRuleCount > 0 && (
                  <section className="space-y-3">
                    <p className="text-muted-foreground text-[11px] font-medium tracking-wider uppercase">
                      Rules
                    </p>
                    <div className="bg-secondary/40 border-border flex items-start gap-3 rounded-md border p-3">
                      <div className="min-w-0 flex-1">
                        <p className="text-foreground text-sm font-medium">
                          {activeRuleCount} rule
                          {activeRuleCount === 1 ? "" : "s"} enabled by default
                        </p>
                        <p className="text-muted-foreground mt-1 text-xs leading-relaxed">
                          {deferRules
                            ? "Too many rules to manage here. Pick specific patterns to enable or disable in the Policy Center."
                            : "All rules in this category are active. Disable individual rules in the Policy Center."}
                        </p>
                        <Link
                          href={policyCenterHref}
                          target="_blank"
                          rel="noopener noreferrer"
                          size="sm"
                          iconSuffixName="external-link"
                          className="mt-2"
                        >
                          Pick specific rules
                        </Link>
                      </div>
                    </div>
                  </section>
                )}
              </div>

              <div className="border-border flex items-center justify-end border-t px-6 py-4">
                <Button
                  variant="primary"
                  size="sm"
                  onClick={() => setOpenCategory(null)}
                >
                  <Button.Text>Done</Button.Text>
                </Button>
              </div>
            </>
          )}
        </SheetContent>
      </Sheet>
    </StepContainer>
  );
}

interface ShadowMcpHeroProps {
  config: CategoryConfig;
  onToggle: (checked: boolean) => void;
}

function ShadowMcpHero({ config, onToggle }: ShadowMcpHeroProps) {
  return (
    <div
      className={cn(
        "relative overflow-hidden rounded-lg backdrop-blur-xl transition-all duration-700 ease-out",
        "bg-gradient-to-br from-slate-500/85 via-slate-600/85 to-slate-700/85",
        "dark:from-slate-600/85 dark:via-slate-700/85 dark:to-slate-800/85",
        "ring-1",
        config.enabled ? "ring-emerald-500/20" : "ring-[var(--bg-warning)]/30",
        "shadow-[0_1px_0_rgba(255,255,255,0.1)_inset,0_0_0_1px_rgba(255,255,255,0.02)_inset,0_2px_4px_rgba(15,23,42,0.12),0_12px_28px_-12px_rgba(15,23,42,0.4),0_24px_56px_-24px_rgba(15,23,42,0.5)]",
      )}
    >
      <div
        aria-hidden
        className={cn(
          "pointer-events-none absolute -top-32 -right-24 h-72 w-72 rounded-full blur-3xl transition-all duration-[1100ms] ease-[cubic-bezier(0.22,1,0.36,1)]",
          config.enabled
            ? "translate-x-[-12%] translate-y-4 scale-150 bg-emerald-500/[0.28]"
            : "translate-x-6 translate-y-[-8%] scale-75 bg-[var(--bg-warning)]/30",
        )}
      />
      <div
        aria-hidden
        className={cn(
          "pointer-events-none absolute -bottom-40 -left-20 h-80 w-80 rounded-full blur-3xl transition-all duration-[1300ms] ease-[cubic-bezier(0.22,1,0.36,1)]",
          config.enabled
            ? "translate-x-8 translate-y-[-10%] scale-150 bg-emerald-500/[0.3]"
            : "translate-x-[-6%] translate-y-4 scale-50 bg-[var(--bg-warning)]/25",
        )}
      />
      <div
        aria-hidden
        className={cn(
          "pointer-events-none absolute top-1/2 left-1/2 h-64 w-64 -translate-x-1/2 -translate-y-1/2 rounded-full blur-3xl transition-all duration-[1500ms] ease-[cubic-bezier(0.22,1,0.36,1)]",
          config.enabled
            ? "scale-125 bg-emerald-500/[0.15] opacity-100"
            : "scale-50 bg-[var(--bg-warning)]/15 opacity-60",
        )}
      />
      <div
        aria-hidden
        className="pointer-events-none absolute inset-x-0 top-0 h-px bg-gradient-to-r from-transparent via-white/25 to-transparent"
      />
      <div
        aria-hidden
        className="pointer-events-none absolute inset-0 rounded-lg bg-gradient-to-b from-white/[0.04] to-transparent"
      />
      <div className="relative flex items-start gap-5 p-6 pb-8">
        <div
          className={cn(
            "flex h-12 w-12 flex-shrink-0 items-center justify-center rounded-md ring-1 backdrop-blur-md transition-all duration-500 ease-out",
            "shadow-[0_1px_0_rgba(255,255,255,0.12)_inset,0_8px_16px_-8px_rgba(15,23,42,0.6)]",
            config.enabled
              ? "bg-white/60 ring-white/50"
              : "bg-white/55 ring-white/40",
          )}
        >
          <svg
            viewBox="0 0 24 24"
            fill="none"
            strokeWidth={1.75}
            strokeLinecap="round"
            strokeLinejoin="round"
            className={cn(
              "h-7 w-7 transition-colors duration-500 ease-out",
              config.enabled ? "text-emerald-600" : "text-[var(--bg-warning)]",
            )}
          >
            <path
              d="M20 13c0 5-3.5 7.5-7.66 8.95a1 1 0 0 1-.67-.01C7.5 20.5 4 18 4 13V6a1 1 0 0 1 1-1c2 0 4.5-1.2 6.24-2.72a1.17 1.17 0 0 1 1.52 0C14.51 3.81 17 5 19 5a1 1 0 0 1 1 1z"
              stroke="currentColor"
            />
            <path
              d="m9 12 2 2 4-4"
              stroke="currentColor"
              strokeDasharray="10"
              strokeDashoffset={config.enabled ? 0 : 10}
              style={{
                transition: config.enabled
                  ? "stroke-dashoffset 250ms ease-out 100ms"
                  : "none",
              }}
            />
          </svg>
        </div>
        <div className="min-w-0 flex-1 space-y-3 pt-0.5">
          <div className="flex items-center gap-2">
            <p className="text-base font-medium tracking-tight text-slate-50">
              Shadow MCP enforcement
            </p>
            <Badge variant={config.enabled ? "success" : "warning"}>
              <Badge.Text>{config.enabled ? "Active" : "Off"}</Badge.Text>
            </Badge>
          </div>
          <p className="max-w-md text-sm leading-relaxed text-slate-300/90">
            Force every MCP tool call through Speakeasy&rsquo;s control plane.
            Unmanaged servers your team installs locally are blocked &mdash; so
            RBAC, authz, and audit trails stay enforced across every agent.
          </p>
        </div>
        <Switch
          checked={config.enabled}
          onCheckedChange={onToggle}
          aria-label="Enable shadow MCP enforcement"
          className={cn(
            "mt-1 shadow-[0_1px_2px_rgba(0,0,0,0.3)_inset]",
            config.enabled
              ? "bg-emerald-500 hover:bg-emerald-500/90"
              : "bg-white/[0.12] hover:bg-white/[0.18]",
          )}
        />
      </div>
    </div>
  );
}

interface ActionRadioProps {
  value: PolicyAction;
  label: string;
  description: string;
  disabled: boolean;
  selected: boolean;
}

function ActionRadio({
  value,
  label,
  description,
  disabled,
  selected,
}: ActionRadioProps) {
  const id = `action-${value}`;
  return (
    <label
      htmlFor={id}
      className={cn(
        "border-border bg-secondary/20 flex cursor-pointer items-start gap-3 rounded-md border p-3 transition-colors",
        selected && !disabled && "border-foreground/40 bg-secondary/50",
        disabled && "cursor-not-allowed opacity-50",
      )}
    >
      <RadioGroupItem id={id} value={value} disabled={disabled} />
      <div className="min-w-0 flex-1">
        <Label htmlFor={id} className="cursor-pointer">
          {label}
        </Label>
        <p className="text-muted-foreground mt-0.5 text-xs leading-relaxed">
          {description}
        </p>
      </div>
    </label>
  );
}

type ActionPillKind = PolicyAction | "off";

const ACTION_PILL_VARIANT: Record<
  ActionPillKind,
  "destructive" | "warning" | "neutral"
> = {
  block: "destructive",
  flag: "warning",
  off: "neutral",
};

const ACTION_PILL_LABEL: Record<ActionPillKind, string> = {
  block: "Block",
  flag: "Flag",
  off: "Off",
};

function ActionPill({ action }: { action: ActionPillKind }) {
  return (
    <Badge variant={ACTION_PILL_VARIANT[action]}>
      <Badge.Text>{ACTION_PILL_LABEL[action]}</Badge.Text>
    </Badge>
  );
}
