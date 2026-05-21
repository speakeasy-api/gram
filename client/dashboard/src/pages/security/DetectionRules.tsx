import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { TextArea } from "@/components/ui/textarea";
import { Type } from "@/components/ui/type";
import { cn } from "@/lib/utils";
import { Icon, type IconName } from "@speakeasy-api/moonshine";
import {
  ArrowLeft,
  ChevronRight,
  Loader2,
  Plus,
  Sparkles,
  Trash2,
} from "lucide-react";
import { useMemo, useState } from "react";
import { toast } from "sonner";
import { useRiskSuggestCustomRuleMutation } from "@gram/client/react-query/index.js";
import {
  BUILTIN_RULES_BY_CATEGORY,
  resolveSeverity,
  SEVERITY_LEVELS,
  SEVERITY_META,
  useDetectionRulesStore,
  validateCustomRuleId,
  validateRegex,
  type BuiltinRule,
  type CustomDetectionRule,
  type SeverityLevel,
} from "./detection-rules-data";
import { RULE_CATEGORY_META, type RuleCategory } from "./policy-data";

/** Presidio-backed categories: kept in the same order the policy form uses
 *  so users see the two surfaces in the same shape. */
const PRESIDIO_CATEGORIES: RuleCategory[] = [
  "financial",
  "pii",
  "government_ids",
  "healthcare",
];

const BUILTIN_CATEGORY_ORDER: RuleCategory[] = [
  "secrets",
  ...PRESIDIO_CATEGORIES,
  "shadow_mcp",
  "destructive_tool",
  "prompt_injection",
];

type SelectedRule =
  | { kind: "builtin"; rule: BuiltinRule }
  | { kind: "custom"; rule: CustomDetectionRule };

export default function DetectionRules() {
  return (
    <RequireScope scope="org:admin" level="page">
      <Page>
        <Page.Header>
          <Page.Header.Breadcrumbs />
        </Page.Header>
        <Page.Body>
          <DetectionRulesContent />
        </Page.Body>
      </Page>
    </RequireScope>
  );
}

function DetectionRulesContent() {
  const {
    severityOverrides,
    customRules,
    setSeverityOverride,
    addCustomRule,
    updateCustomRule,
    removeCustomRule,
  } = useDetectionRulesStore();

  const [createOpen, setCreateOpen] = useState(false);
  const [selected, setSelected] = useState<SelectedRule | null>(null);
  const [expanded, setExpanded] = useState<RuleCategory | "custom" | null>(
    null,
  );

  return (
    <>
      <Page.Section>
        <Page.Section.Title stage="beta">Detection Rules</Page.Section.Title>
        <Page.Section.Description>
          Built-in detection rules grouped by category. Click a rule to view its
          description and override the default severity, or add your own custom
          regex rule.
        </Page.Section.Description>
        <Page.Section.CTA>
          <Button onClick={() => setCreateOpen(true)}>
            <Plus className="mr-2 h-4 w-4" />
            New Custom Detection Rule
          </Button>
        </Page.Section.CTA>
        <Page.Section.Body>
          <div className="space-y-8">
            {customRules.length > 0 && (
              <CustomRulesSection
                rules={customRules}
                expanded={expanded === "custom"}
                onToggle={() =>
                  setExpanded(expanded === "custom" ? null : "custom")
                }
                onSelect={(rule) => setSelected({ kind: "custom", rule })}
              />
            )}

            <BuiltinRulesSection
              severityOverrides={severityOverrides}
              expanded={expanded}
              onToggle={(cat) => setExpanded(expanded === cat ? null : cat)}
              onSelect={(rule) => setSelected({ kind: "builtin", rule })}
            />
          </div>
        </Page.Section.Body>
      </Page.Section>

      <RuleDetailSheet
        selection={selected}
        severityOverrides={severityOverrides}
        onClose={() => setSelected(null)}
        onOverrideSeverity={setSeverityOverride}
        onUpdateCustomRule={updateCustomRule}
        onDeleteCustomRule={(id) => {
          removeCustomRule(id);
          setSelected(null);
          toast.success("Custom detection rule deleted");
        }}
      />

      <CreateCustomRuleSheet
        open={createOpen}
        onOpenChange={setCreateOpen}
        existingCustomIds={customRules.map((r) => r.id)}
        onCreate={(rule) => {
          addCustomRule(rule);
          setCreateOpen(false);
          toast.success(`Created custom rule ${rule.id}`);
        }}
      />
    </>
  );
}

/* -------------------------------------------------------------------------- */
/*  Custom rules section                                                       */
/* -------------------------------------------------------------------------- */

function CustomRulesSection({
  rules,
  expanded,
  onToggle,
  onSelect,
}: {
  rules: CustomDetectionRule[];
  expanded: boolean;
  onToggle: () => void;
  onSelect: (rule: CustomDetectionRule) => void;
}) {
  const meta = RULE_CATEGORY_META.custom;
  return (
    <div>
      <Type variant="subheading" className="mb-3">
        Custom
      </Type>
      <div className="border-border divide-border divide-y rounded-lg border">
        <CategoryHeader
          icon={meta.icon as IconName}
          label="Custom Patterns"
          description={`${rules.length} organization-defined rule${rules.length === 1 ? "" : "s"}`}
          expanded={expanded}
          onClick={onToggle}
          count={rules.length}
        />
        {expanded && (
          <div className="bg-muted/30 divide-border divide-y">
            {rules.map((rule) => (
              <RuleRow
                key={rule.id}
                title={rule.title || rule.id}
                subtitle={rule.id}
                severity={rule.severity}
                onClick={() => onSelect(rule)}
              />
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

/* -------------------------------------------------------------------------- */
/*  Builtin rules section                                                      */
/* -------------------------------------------------------------------------- */

function BuiltinRulesSection({
  severityOverrides,
  expanded,
  onToggle,
  onSelect,
}: {
  severityOverrides: Record<string, SeverityLevel>;
  expanded: RuleCategory | "custom" | null;
  onToggle: (cat: RuleCategory) => void;
  onSelect: (rule: BuiltinRule) => void;
}) {
  return (
    <div>
      <Type variant="subheading" className="mb-3">
        Built-in
      </Type>
      <div className="border-border divide-border divide-y rounded-lg border">
        {BUILTIN_CATEGORY_ORDER.map((cat) => {
          const meta = RULE_CATEGORY_META[cat];
          const rules = BUILTIN_RULES_BY_CATEGORY[cat];
          const isExpanded = expanded === cat;
          return (
            <div key={cat}>
              <CategoryHeader
                icon={meta.icon as IconName}
                label={meta.label}
                description={meta.description}
                expanded={isExpanded}
                onClick={() => onToggle(cat)}
                count={rules.length}
              />
              {isExpanded && rules.length > 0 && (
                <div className="bg-muted/30 divide-border divide-y">
                  {rules.map((rule) => {
                    const severity = resolveSeverity(
                      rule.id,
                      rule.defaultSeverity,
                      severityOverrides,
                    );
                    const isOverridden =
                      severityOverrides[rule.id] !== undefined &&
                      severityOverrides[rule.id] !== rule.defaultSeverity;
                    return (
                      <RuleRow
                        key={rule.id}
                        title={rule.title}
                        subtitle={rule.id}
                        severity={severity}
                        overridden={isOverridden}
                        onClick={() => onSelect(rule)}
                      />
                    );
                  })}
                </div>
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
}

/* -------------------------------------------------------------------------- */
/*  Shared row components                                                      */
/* -------------------------------------------------------------------------- */

function CategoryHeader({
  icon,
  label,
  description,
  expanded,
  count,
  onClick,
}: {
  icon: IconName;
  label: string;
  description: string;
  expanded: boolean;
  count: number;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="hover:bg-muted/40 flex w-full items-center gap-3 px-4 py-3 text-left transition-colors"
    >
      <ChevronRight
        className={cn(
          "text-muted-foreground h-4 w-4 shrink-0 transition-transform",
          expanded && "rotate-90",
        )}
      />
      <Icon name={icon} className="text-muted-foreground size-4 shrink-0" />
      <div className="min-w-0 flex-1">
        <div className="text-sm font-medium">{label}</div>
        <div className="text-muted-foreground line-clamp-1 text-xs">
          {description}
        </div>
      </div>
      <Badge variant="secondary" className="shrink-0">
        {count}
      </Badge>
    </button>
  );
}

function RuleRow({
  title,
  subtitle,
  severity,
  overridden,
  onClick,
}: {
  title: string;
  subtitle: string;
  severity: SeverityLevel;
  overridden?: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="hover:bg-muted/40 flex w-full items-center gap-3 px-4 py-2.5 pl-11 text-left transition-colors"
    >
      <div className="min-w-0 flex-1">
        <div className="truncate text-sm">{title}</div>
        <div className="text-muted-foreground truncate font-mono text-[11px]">
          {subtitle}
        </div>
      </div>
      <SeverityBadge severity={severity} />
      {overridden && (
        <Badge variant="outline" className="text-[10px]">
          Override
        </Badge>
      )}
      <ChevronRight className="text-muted-foreground size-4 shrink-0" />
    </button>
  );
}

export function SeverityBadge({ severity }: { severity: SeverityLevel }) {
  const meta = SEVERITY_META[severity];
  return (
    <span
      className={cn(
        "inline-flex items-center rounded-md border px-2 py-0.5 text-[11px] font-medium",
        meta.badgeClass,
      )}
    >
      {meta.label}
    </span>
  );
}

/* -------------------------------------------------------------------------- */
/*  Rule detail sheet                                                          */
/* -------------------------------------------------------------------------- */

function RuleDetailSheet({
  selection,
  severityOverrides,
  onClose,
  onOverrideSeverity,
  onUpdateCustomRule,
  onDeleteCustomRule,
}: {
  selection: SelectedRule | null;
  severityOverrides: Record<string, SeverityLevel>;
  onClose: () => void;
  onOverrideSeverity: (ruleId: string, severity: SeverityLevel | null) => void;
  onUpdateCustomRule: (
    id: string,
    patch: Partial<Omit<CustomDetectionRule, "id" | "createdAt">>,
  ) => void;
  onDeleteCustomRule: (id: string) => void;
}) {
  return (
    <Sheet open={!!selection} onOpenChange={(open) => !open && onClose()}>
      <SheetContent className="flex flex-col overflow-y-auto sm:max-w-lg">
        {selection?.kind === "builtin" && (
          <BuiltinRuleDetail
            rule={selection.rule}
            override={severityOverrides[selection.rule.id]}
            onOverride={(severity) =>
              onOverrideSeverity(selection.rule.id, severity)
            }
          />
        )}
        {selection?.kind === "custom" && (
          <CustomRuleDetail
            rule={selection.rule}
            onUpdate={(patch) => onUpdateCustomRule(selection.rule.id, patch)}
            onDelete={() => onDeleteCustomRule(selection.rule.id)}
          />
        )}
      </SheetContent>
    </Sheet>
  );
}

function BuiltinRuleDetail({
  rule,
  override,
  onOverride,
}: {
  rule: BuiltinRule;
  override: SeverityLevel | undefined;
  onOverride: (severity: SeverityLevel | null) => void;
}) {
  const meta = RULE_CATEGORY_META[rule.category];
  const effective = override ?? rule.defaultSeverity;
  return (
    <>
      <SheetHeader className="px-6 pt-6">
        <SheetTitle>{rule.title}</SheetTitle>
        <SheetDescription className="font-mono text-xs">
          {rule.id}
        </SheetDescription>
      </SheetHeader>
      <div className="flex-1 space-y-6 px-6 py-4">
        <DetailField label="Category">
          <div className="flex items-center gap-2">
            <Icon
              name={meta.icon as IconName}
              className="text-muted-foreground size-4"
            />
            <span className="text-sm">{meta.label}</span>
          </div>
        </DetailField>

        <DetailField label="Description">
          <p className="text-sm leading-relaxed">{rule.description}</p>
        </DetailField>

        <DetailField label="Default severity">
          <div className="flex items-center gap-2">
            <SeverityBadge severity={rule.defaultSeverity} />
            <span className="text-muted-foreground text-xs">
              {SEVERITY_META[rule.defaultSeverity].description}
            </span>
          </div>
        </DetailField>

        <DetailField label="Override severity">
          <div className="flex items-center gap-2">
            <Select
              value={effective}
              onValueChange={(v) =>
                onOverride(
                  v === rule.defaultSeverity ? null : (v as SeverityLevel),
                )
              }
            >
              <SelectTrigger className="w-[160px]">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {SEVERITY_LEVELS.map((level) => (
                  <SelectItem key={level} value={level}>
                    {SEVERITY_META[level].label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            {override !== undefined && override !== rule.defaultSeverity && (
              <Button
                variant="ghost"
                size="sm"
                onClick={() => onOverride(null)}
              >
                Reset to default
              </Button>
            )}
          </div>
          <p className="text-muted-foreground mt-2 text-xs">
            Overrides change how findings for this rule render in dashboards and
            risk reports.
          </p>
        </DetailField>
      </div>
    </>
  );
}

function CustomRuleDetail({
  rule,
  onUpdate,
  onDelete,
}: {
  rule: CustomDetectionRule;
  onUpdate: (
    patch: Partial<Omit<CustomDetectionRule, "id" | "createdAt">>,
  ) => void;
  onDelete: () => void;
}) {
  const [title, setTitle] = useState(rule.title);
  const [description, setDescription] = useState(rule.description);
  const [regex, setRegex] = useState(rule.regex);
  const [severity, setSeverity] = useState<SeverityLevel>(rule.severity);

  const regexError = useMemo(() => validateRegex(regex), [regex]);
  const dirty =
    title !== rule.title ||
    description !== rule.description ||
    regex !== rule.regex ||
    severity !== rule.severity;

  return (
    <>
      <SheetHeader className="px-6 pt-6">
        <SheetTitle>{rule.title || rule.id}</SheetTitle>
        <SheetDescription className="font-mono text-xs">
          {rule.id}
        </SheetDescription>
      </SheetHeader>
      <div className="flex-1 space-y-5 px-6 py-4">
        <div className="space-y-2">
          <Label className="text-sm font-medium">Title</Label>
          <Input value={title} onChange={setTitle} />
        </div>

        <div className="space-y-2">
          <Label className="text-sm font-medium">Description</Label>
          <TextArea
            value={description}
            onChange={setDescription}
            rows={3}
            placeholder="What this rule detects and why it matters"
          />
        </div>

        <div className="space-y-2">
          <Label className="text-sm font-medium">Regex</Label>
          <TextArea
            value={regex}
            onChange={setRegex}
            rows={3}
            className="font-mono text-xs"
          />
          {regexError && (
            <p className="text-destructive text-xs">{regexError}</p>
          )}
        </div>

        <div className="space-y-2">
          <Label className="text-sm font-medium">Severity</Label>
          <Select
            value={severity}
            onValueChange={(v) => setSeverity(v as SeverityLevel)}
          >
            <SelectTrigger className="w-[160px]">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {SEVERITY_LEVELS.map((level) => (
                <SelectItem key={level} value={level}>
                  {SEVERITY_META[level].label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      </div>
      <SheetFooter className="border-border flex-row justify-between border-t px-6 py-4">
        <Button
          variant="ghost"
          size="sm"
          onClick={onDelete}
          className="text-destructive hover:text-destructive"
        >
          <Trash2 className="mr-2 h-4 w-4" />
          Delete rule
        </Button>
        <Button
          disabled={!dirty || !!regexError || !title.trim()}
          onClick={() => onUpdate({ title, description, regex, severity })}
        >
          Save changes
        </Button>
      </SheetFooter>
    </>
  );
}

function DetailField({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <div>
      <div className="text-muted-foreground mb-2 text-xs font-medium tracking-wide uppercase">
        {label}
      </div>
      {children}
    </div>
  );
}

/* -------------------------------------------------------------------------- */
/*  Create custom rule sheet                                                   */
/* -------------------------------------------------------------------------- */

type CreateStep = "prompt" | "review";

function CreateCustomRuleSheet({
  open,
  onOpenChange,
  existingCustomIds,
  onCreate,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  existingCustomIds: string[];
  onCreate: (rule: {
    id: string;
    title: string;
    description: string;
    regex: string;
    severity: SeverityLevel;
  }) => void;
}) {
  const [step, setStep] = useState<CreateStep>("prompt");
  const [askPrompt, setAskPrompt] = useState("");
  const [id, setId] = useState("");
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [regex, setRegex] = useState("");
  const [severity, setSeverity] = useState<SeverityLevel>("medium");

  const reset = () => {
    setStep("prompt");
    setAskPrompt("");
    setId("");
    setTitle("");
    setDescription("");
    setRegex("");
    setSeverity("medium");
  };

  const suggestMutation = useRiskSuggestCustomRuleMutation({
    onSuccess: (data) => {
      const next = data;
      setId(next.ruleId);
      setTitle(next.title);
      setDescription(next.description);
      setRegex(next.regex);
      setSeverity(
        (SEVERITY_LEVELS as readonly string[]).includes(next.severity)
          ? (next.severity as SeverityLevel)
          : "medium",
      );
      setStep("review");
    },
    onError: (err) => {
      const message =
        err instanceof Error ? err.message : "Failed to generate suggestion";
      toast.error(message);
    },
  });

  const handleSuggest = () => {
    const prompt = askPrompt.trim();
    if (prompt.length < 3) {
      toast.error("Tell us what you want to detect first.");
      return;
    }
    suggestMutation.mutate({
      request: {
        suggestCustomDetectionRuleRequestBody: {
          prompt,
          existingRuleIds: existingCustomIds,
        },
      },
    });
  };

  const handleManual = () => {
    setId("");
    setTitle("");
    setDescription("");
    setRegex("");
    setSeverity("medium");
    setStep("review");
  };

  const idError = useMemo(
    () => (id ? validateCustomRuleId(id, existingCustomIds) : null),
    [id, existingCustomIds],
  );
  const regexError = useMemo(
    () => (regex ? validateRegex(regex) : null),
    [regex],
  );

  const canSubmit =
    id.trim() && title.trim() && regex.trim() && !idError && !regexError;

  const handleSubmit = () => {
    const finalIdError = validateCustomRuleId(id, existingCustomIds);
    if (finalIdError) {
      toast.error(finalIdError);
      return;
    }
    const finalRegexError = validateRegex(regex);
    if (finalRegexError) {
      toast.error(finalRegexError);
      return;
    }
    onCreate({
      id: id.trim(),
      title: title.trim(),
      description: description.trim(),
      regex: regex.trim(),
      severity,
    });
    reset();
  };

  return (
    <Sheet
      open={open}
      onOpenChange={(next) => {
        onOpenChange(next);
        if (!next) reset();
      }}
    >
      <SheetContent className="flex flex-col overflow-y-auto sm:max-w-lg">
        <SheetHeader className="px-6 pt-6">
          <SheetTitle>New Custom Detection Rule</SheetTitle>
          <SheetDescription>
            {step === "prompt"
              ? "Describe what you want to detect. We'll suggest the rule ID, regex, and severity, you tweak before saving."
              : "Review the suggested rule. Adjust any field before saving."}
          </SheetDescription>
        </SheetHeader>

        {step === "prompt" ? (
          <>
            <div className="flex-1 space-y-5 px-6 py-4">
              <div className="space-y-2">
                <Label className="text-sm font-medium">
                  What do you want to detect?
                </Label>
                <TextArea
                  value={askPrompt}
                  onChange={setAskPrompt}
                  rows={5}
                  placeholder="e.g. internal Acme service tokens that look like acme_ followed by 32 lowercase hex characters"
                  autoFocus
                />
                <p className="text-muted-foreground text-xs">
                  Tip: include a sample value or the format so the model picks a
                  tight regex.
                </p>
              </div>
            </div>
            <SheetFooter className="border-border flex-row items-center justify-between border-t px-6 py-4">
              <Button variant="ghost" size="sm" onClick={handleManual}>
                Skip, fill manually
              </Button>
              <Button
                disabled={
                  askPrompt.trim().length < 3 || suggestMutation.isPending
                }
                onClick={handleSuggest}
              >
                {suggestMutation.isPending ? (
                  <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                ) : (
                  <Sparkles className="mr-2 h-4 w-4" />
                )}
                Suggest with AI
              </Button>
            </SheetFooter>
          </>
        ) : (
          <>
            <div className="flex-1 space-y-5 px-6 py-4">
              <div className="space-y-2">
                <Label className="text-sm font-medium">Rule ID</Label>
                <Input
                  value={id}
                  onChange={setId}
                  placeholder="e.g. custom.internal_token"
                  className="font-mono text-xs"
                />
                {idError ? (
                  <p className="text-destructive text-xs">{idError}</p>
                ) : (
                  <p className="text-muted-foreground text-xs">
                    Stable identifier used in policies and findings. Cannot
                    collide with built-in or existing custom rules.
                  </p>
                )}
              </div>

              <div className="space-y-2">
                <Label className="text-sm font-medium">Title</Label>
                <Input
                  value={title}
                  onChange={setTitle}
                  placeholder="e.g. Internal API Token"
                />
              </div>

              <div className="space-y-2">
                <Label className="text-sm font-medium">Description</Label>
                <TextArea
                  value={description}
                  onChange={setDescription}
                  rows={3}
                  placeholder="Explain what this rule detects so reviewers know how to act on findings"
                />
              </div>

              <div className="space-y-2">
                <Label className="text-sm font-medium">Regex</Label>
                <TextArea
                  value={regex}
                  onChange={setRegex}
                  rows={3}
                  className="font-mono text-xs"
                  placeholder="e.g. acme_[a-z0-9]{32}"
                />
                {regexError ? (
                  <p className="text-destructive text-xs">{regexError}</p>
                ) : (
                  <p className="text-muted-foreground text-xs">
                    Anchors are not required, the pattern is matched against the
                    full scanned payload.
                  </p>
                )}
              </div>

              <div className="space-y-2">
                <Label className="text-sm font-medium">Default severity</Label>
                <Select
                  value={severity}
                  onValueChange={(v) => setSeverity(v as SeverityLevel)}
                >
                  <SelectTrigger className="w-[160px]">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {SEVERITY_LEVELS.map((level) => (
                      <SelectItem key={level} value={level}>
                        {SEVERITY_META[level].label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            </div>
            <SheetFooter className="border-border flex-row items-center justify-between border-t px-6 py-4">
              <Button
                variant="ghost"
                size="sm"
                onClick={() => setStep("prompt")}
              >
                <ArrowLeft className="mr-2 h-4 w-4" />
                Back
              </Button>
              <Button disabled={!canSubmit} onClick={handleSubmit}>
                Create rule
              </Button>
            </SheetFooter>
          </>
        )}
      </SheetContent>
    </Sheet>
  );
}
