import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
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
  Check,
  ChevronRight,
  Loader2,
  Plus,
  Sparkles,
  Trash2,
} from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { useQueryState } from "nuqs";
import { toast } from "sonner";
import {
  useListChats,
  useRiskSuggestCustomRuleMutation,
  useRiskTestDetectionRuleMutation,
} from "@gram/client/react-query/index.js";
import type { ChatOverview } from "@gram/client/models/components/chatoverview.js";
import type { TestDetectionRuleMatch } from "@gram/client/models/components/testdetectionrulematch.js";
import type { TestDetectionRuleResult } from "@gram/client/models/components/testdetectionruleresult.js";
import { chatLoad } from "@gram/client/funcs/chatLoad.js";
import { unwrapAsync } from "@gram/client/types/fp.js";
import { useSdkClient } from "@/contexts/Sdk";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import {
  BUILTIN_RULES_BY_CATEGORY,
  SEVERITY_LEVELS,
  buildMatchConfig,
  ruleCombine,
  ruleConditions,
  ruleSummary,
  useDetectionRulesStore,
  validateCustomRuleId,
  type BuiltinRule,
  type CustomDetectionRule,
  type CustomRuleDraft,
  type SeverityLevel,
} from "./detection-rules-data";
import { matchQueryFromConditions, parseMatchQuery } from "./match-query";
import { MatchQueryMonaco as MatchQueryInput } from "./match-query-monaco";
import type { RiskMatchConfig } from "@gram/client/models/components";
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

const CUSTOM_RULE_ID_PREFIX = "custom.";

type SelectedRule =
  | { kind: "builtin"; rule: BuiltinRule }
  | { kind: "custom"; rule: CustomDetectionRule };

export default function DetectionRules(): JSX.Element {
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
    customRules,
    isLoading: customRulesLoading,
    error: customRulesError,
    addCustomRule,
    updateCustomRule,
    removeCustomRule,
  } = useDetectionRulesStore();

  const [createOpen, setCreateOpen] = useState(false);
  const [selected, setSelected] = useState<SelectedRule | null>(null);
  const [expanded, setExpanded] = useState<RuleCategory | "custom" | null>(
    null,
  );

  // Deep-link support: `?rule=<id>` opens that rule's detail sheet (and expands
  // its category). The command palette uses this since rules have no per-item
  // route. The `custom.` prefix distinguishes custom rules from built-ins, so
  // no separate kind param is needed.
  const [ruleParam, setRuleParam] = useQueryState("rule");
  const openedRuleRef = useRef<string | null>(null);
  useEffect(() => {
    if (!ruleParam || openedRuleRef.current === ruleParam) return;
    if (ruleParam.startsWith(CUSTOM_RULE_ID_PREFIX)) {
      if (customRulesLoading) return;
      const rule = customRules.find((r) => r.id === ruleParam);
      // Mark handled regardless of match so a stale/invalid id doesn't re-run
      // the lookup on every `customRules` re-render (it's a fresh array ref).
      openedRuleRef.current = ruleParam;
      if (rule) {
        setSelected({ kind: "custom", rule });
        setExpanded("custom");
      }
    } else {
      const rule = Object.values(BUILTIN_RULES_BY_CATEGORY)
        .flat()
        .find((r) => r.id === ruleParam);
      openedRuleRef.current = ruleParam;
      if (rule) {
        setSelected({ kind: "builtin", rule });
        setExpanded(rule.category);
      }
    }
  }, [ruleParam, customRules, customRulesLoading]);

  const clearRuleDeepLink = () => {
    openedRuleRef.current = null;
    void setRuleParam(null);
  };

  return (
    <>
      <Page.Section>
        <Page.Section.Title stage="beta">Detection Rules</Page.Section.Title>
        <Page.Section.Description>
          Reusable built-in and custom rules your policies use to flag — or
          exempt — messages.
        </Page.Section.Description>
        <Page.Section.CTA>
          <Button onClick={() => setCreateOpen(true)}>
            <Plus className="mr-2 h-4 w-4" />
            Custom Detection Rule
          </Button>
        </Page.Section.CTA>
        <Page.Section.Body>
          <div className="space-y-8">
            {customRulesLoading && (
              <div className="text-muted-foreground text-sm">
                Loading custom rules...
              </div>
            )}
            {customRulesError && (
              <div className="text-destructive text-sm">
                Failed to load custom rules.
              </div>
            )}
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
              expanded={expanded}
              onToggle={(cat) => setExpanded(expanded === cat ? null : cat)}
              onSelect={(rule) => setSelected({ kind: "builtin", rule })}
            />
          </div>
        </Page.Section.Body>
      </Page.Section>

      <RuleDetailSheet
        selection={selected}
        onClose={() => {
          setSelected(null);
          clearRuleDeepLink();
        }}
        onUpdateCustomRule={updateCustomRule}
        onDeleteCustomRule={(id) => {
          removeCustomRule(id);
          setSelected(null);
          clearRuleDeepLink();
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
          label={meta.label}
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
                subtitle={ruleSummary(rule)}
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
  expanded,
  onToggle,
  onSelect,
}: {
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
                  {rules.map((rule) => (
                    <RuleRow
                      key={rule.id}
                      title={rule.title}
                      subtitle={rule.id}
                      onClick={() => onSelect(rule)}
                    />
                  ))}
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
  onClick,
}: {
  title: string;
  subtitle: string;
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
      <ChevronRight className="text-muted-foreground size-4 shrink-0" />
    </button>
  );
}

/* -------------------------------------------------------------------------- */
/*  Rule detail sheet                                                          */
/* -------------------------------------------------------------------------- */

function RuleDetailSheet({
  selection,
  onClose,
  onUpdateCustomRule,
  onDeleteCustomRule,
}: {
  selection: SelectedRule | null;
  onClose: () => void;
  onUpdateCustomRule: (
    id: string,
    patch: Partial<Omit<CustomDetectionRule, "id" | "dbId" | "createdAt">>,
  ) => Promise<void>;
  onDeleteCustomRule: (id: string) => void;
}) {
  return (
    <Sheet
      open={!!selection}
      onOpenChange={(open) => {
        void (!open && onClose());
      }}
    >
      <SheetContent className="flex flex-col overflow-y-auto sm:max-w-xl">
        {selection?.kind === "builtin" && (
          <BuiltinRuleDetail rule={selection.rule} />
        )}
        {selection?.kind === "custom" && (
          <CustomRuleDetail
            key={selection.rule.id}
            rule={selection.rule}
            onUpdate={(patch) => onUpdateCustomRule(selection.rule.id, patch)}
            onDelete={() => onDeleteCustomRule(selection.rule.id)}
          />
        )}
      </SheetContent>
    </Sheet>
  );
}

function BuiltinRuleDetail({ rule }: { rule: BuiltinRule }) {
  const meta = RULE_CATEGORY_META[rule.category];
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

        <RulePlayground ruleId={rule.id} matchConfig={null} />
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
  onUpdate: (patch: Partial<Omit<CustomRuleDraft, "id">>) => Promise<void>;
  onDelete: () => void;
}) {
  const [title, setTitle] = useState(rule.title);
  const [description, setDescription] = useState(rule.description);
  const [query, setQuery] = useState(() =>
    matchQueryFromConditions(ruleConditions(rule), ruleCombine(rule)),
  );
  const [saveState, setSaveState] = useState<
    "idle" | "saving" | "saved" | "error"
  >("idle");
  const savedTimerRef = useRef<number | undefined>(undefined);
  const savedRef = useRef({
    title: rule.title,
    description: rule.description,
    query: matchQueryFromConditions(ruleConditions(rule), ruleCombine(rule)),
  });

  const parsed = useMemo(() => parseMatchQuery(query), [query]);
  const dirty =
    title !== savedRef.current.title ||
    description !== savedRef.current.description ||
    query !== savedRef.current.query;

  useEffect(() => {
    if (dirty && saveState === "error") {
      setSaveState("idle");
    }
  }, [dirty, saveState]);

  useEffect(
    () => () => {
      if (savedTimerRef.current !== undefined) {
        window.clearTimeout(savedTimerRef.current);
      }
    },
    [],
  );

  const handleSave = async () => {
    if (parsed.error) {
      toast.error(parsed.error);
      return;
    }
    setSaveState("saving");
    try {
      await onUpdate({
        title,
        description,
        conditions: parsed.conditions,
        combine: parsed.combine,
      });
      savedRef.current = { title, description, query };
      setSaveState("saved");
      if (savedTimerRef.current !== undefined) {
        window.clearTimeout(savedTimerRef.current);
      }
      savedTimerRef.current = window.setTimeout(() => {
        setSaveState("idle");
      }, 1800);
    } catch {
      setSaveState("error");
    }
  };

  const saveLabel =
    saveState === "saving"
      ? "Saving..."
      : saveState === "saved"
        ? "Saved"
        : "Save changes";

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
          <Label className="text-sm font-medium">Match conditions</Label>
          <MatchQueryInput
            value={query}
            onChange={setQuery}
            error={query.trim() ? parsed.error : null}
          />
        </div>

        <RulePlayground
          ruleId={rule.id}
          matchConfig={
            parsed.error
              ? null
              : buildMatchConfig(parsed.conditions, parsed.combine)
          }
        />
      </div>
      <SheetFooter className="border-border flex-row items-center justify-between border-t px-6 py-4">
        <Button
          variant="ghost"
          size="sm"
          onClick={onDelete}
          className="text-destructive hover:text-destructive"
        >
          <Trash2 className="mr-2 h-4 w-4" />
          Delete rule
        </Button>
        <div className="flex items-center gap-3">
          {saveState === "error" && (
            <span className="text-destructive text-xs">Could not save</span>
          )}
          <Button
            disabled={
              !dirty ||
              !!parsed.error ||
              !title.trim() ||
              saveState === "saving"
            }
            onClick={() => void handleSave()}
          >
            {saveState === "saving" && (
              <Loader2 className="mr-2 h-4 w-4 animate-spin" />
            )}
            {saveState === "saved" && <Check className="mr-2 h-4 w-4" />}
            {saveLabel}
          </Button>
        </div>
      </SheetFooter>
    </>
  );
}

/* -------------------------------------------------------------------------- */
/*  Rule playground — paste sample text, run the rule via the scanner API     */
/* -------------------------------------------------------------------------- */

function RulePlayground({
  ruleId,
  matchConfig,
}: {
  ruleId: string;
  matchConfig: RiskMatchConfig | null;
}) {
  const [mode, setMode] = useState<"sample" | "chat">("sample");

  return (
    <DetailField label="Playground">
      <p className="text-muted-foreground mb-3 text-xs">
        Run this rule with the same scanner code the worker uses. Paste a sample
        or pick an existing chat.
      </p>
      <RadioGroup
        value={mode}
        onValueChange={(v) => setMode(v as "sample" | "chat")}
        className="mb-4 flex gap-4"
      >
        <label
          htmlFor={`pg-mode-sample-${ruleId}`}
          className="hover:bg-muted/40 flex cursor-pointer items-center gap-2 rounded-md border px-3 py-1.5 text-xs"
        >
          <RadioGroupItem value="sample" id={`pg-mode-sample-${ruleId}`} />
          Paste sample
        </label>
        <label
          htmlFor={`pg-mode-chat-${ruleId}`}
          className="hover:bg-muted/40 flex cursor-pointer items-center gap-2 rounded-md border px-3 py-1.5 text-xs"
        >
          <RadioGroupItem value="chat" id={`pg-mode-chat-${ruleId}`} />
          Run on a chat
        </label>
      </RadioGroup>

      {mode === "sample" ? (
        <SamplePlayground ruleId={ruleId} matchConfig={matchConfig} />
      ) : (
        <ChatPlayground ruleId={ruleId} matchConfig={matchConfig} />
      )}
    </DetailField>
  );
}

function SamplePlayground({
  ruleId,
  matchConfig,
}: {
  ruleId: string;
  matchConfig: RiskMatchConfig | null;
}) {
  const [sample, setSample] = useState("");
  const [matches, setMatches] = useState<TestDetectionRuleMatch[] | null>(null);
  const [reason, setReason] = useState<string | null>(null);

  const mutation = useRiskTestDetectionRuleMutation({
    onSuccess: (data) => {
      setMatches(data.matches);
      setReason(data.supported ? null : (data.reason ?? "Rule not supported"));
    },
    onError: (err) => {
      const message = err instanceof Error ? err.message : "Failed to run rule";
      toast.error(message);
    },
  });

  const handleRun = () => {
    setMatches(null);
    setReason(null);
    mutation.mutate({
      request: {
        testDetectionRuleRequestBody: {
          ruleId,
          text: sample,
          ...(matchConfig ? { matchConfig } : {}),
        },
      },
    });
  };

  return (
    <>
      <TextArea
        value={sample}
        onChange={setSample}
        rows={4}
        placeholder="Paste a sample, an MCP payload, or a chat message snippet…"
      />
      <div className="mt-2 flex justify-end">
        <Button
          size="sm"
          disabled={sample.trim().length === 0 || mutation.isPending}
          onClick={handleRun}
        >
          {mutation.isPending ? (
            <Loader2 className="mr-2 h-4 w-4 animate-spin" />
          ) : null}
          Run rule
        </Button>
      </div>
      {matches !== null && <MatchList matches={matches} reason={reason} />}
    </>
  );
}

function MatchList({
  matches,
  reason,
}: {
  matches: TestDetectionRuleMatch[];
  reason: string | null;
}) {
  return (
    <div className="border-border mt-3 rounded-lg border">
      <div className="border-border bg-muted/40 flex items-center justify-between border-b px-3 py-2 text-xs font-medium">
        <span>
          {matches.length} match{matches.length === 1 ? "" : "es"}
        </span>
        {reason && <span className="text-muted-foreground">{reason}</span>}
      </div>
      {matches.length === 0 ? (
        <p className="text-muted-foreground px-3 py-4 text-xs">
          No findings for this rule.
        </p>
      ) : (
        <ul className="divide-border divide-y">
          {matches.map((m, idx) => (
            <li key={idx} className="px-3 py-2 text-xs">
              <div className="text-muted-foreground mb-1 flex items-center justify-between font-mono">
                <span>{m.ruleId}</span>
                <span>
                  {m.startPos}–{m.endPos} · conf {m.confidence.toFixed(2)} ·{" "}
                  {m.source}
                </span>
              </div>
              <pre className="bg-muted/50 overflow-x-auto rounded px-2 py-1 font-mono text-[11px]">
                {m.match}
              </pre>
              {m.description && (
                <p className="text-muted-foreground mt-1">{m.description}</p>
              )}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

/* -------------------------------------------------------------------------- */
/*  Chat-mode playground                                                       */
/* -------------------------------------------------------------------------- */

const CHAT_WINDOW_MS = 7 * 24 * 60 * 60 * 1000;
const CHAT_MESSAGE_CAP = 100;

type ChatMessageResult = {
  messageId: string;
  role: string;
  textPreview: string;
  fullText: string;
  createdAt: Date | string;
  status: "pending" | "done" | "error";
  result?: TestDetectionRuleResult;
  errorMessage?: string;
};

function ChatPlayground({
  ruleId,
  matchConfig,
}: {
  ruleId: string;
  matchConfig: RiskMatchConfig | null;
}) {
  const client = useSdkClient();
  const chatsQuery = useListChats(undefined, undefined, {
    throwOnError: false,
  });

  const recentChats = useMemo(() => {
    const cutoff = Date.now() - CHAT_WINDOW_MS;
    return (chatsQuery.data?.chats ?? []).filter((c) => {
      const ts = c.lastMessageTimestamp
        ? new Date(c.lastMessageTimestamp).getTime()
        : 0;
      return ts >= cutoff;
    });
  }, [chatsQuery.data]);

  const byUser = useMemo(() => {
    const map = new Map<string, ChatOverview[]>();
    for (const chat of recentChats) {
      const key =
        chat.externalUserId?.trim() ||
        chat.userId?.trim() ||
        "(no external user)";
      const list = map.get(key) ?? [];
      list.push(chat);
      map.set(key, list);
    }
    for (const list of map.values()) {
      list.sort(
        (a, b) =>
          timestampMs(b.lastMessageTimestamp) -
          timestampMs(a.lastMessageTimestamp),
      );
    }
    return Array.from(map.entries()).sort((a, b) => a[0].localeCompare(b[0]));
  }, [recentChats]);

  const [selectedUser, setSelectedUser] = useState<string | null>(null);
  const [selectedChat, setSelectedChat] = useState<string | null>(null);
  const [running, setRunning] = useState(false);
  const [overflowWarning, setOverflowWarning] = useState<string | null>(null);
  const [results, setResults] = useState<ChatMessageResult[]>([]);

  const handleRun = async () => {
    if (!selectedChat) return;
    setRunning(true);
    setResults([]);
    setOverflowWarning(null);
    try {
      const chat = await unwrapAsync(chatLoad(client, { id: selectedChat }));
      const sorted = [...chat.messages].sort(
        (a, b) => timestampMs(b.createdAt) - timestampMs(a.createdAt),
      );
      if (sorted.length > CHAT_MESSAGE_CAP) {
        setOverflowWarning(
          `Chat has ${sorted.length} messages, only the most recent ${CHAT_MESSAGE_CAP} will be analyzed.`,
        );
      }
      const slice = sorted.slice(0, CHAT_MESSAGE_CAP);
      const initial: ChatMessageResult[] = slice.map((m) => {
        const fullText = stringifyMessage(m);
        return {
          messageId: m.id,
          role: m.role,
          textPreview: fullText.slice(0, 240),
          fullText,
          createdAt: m.createdAt,
          status: "pending",
        };
      });
      setResults(initial);

      for (const item of initial) {
        if (!item.fullText.trim()) {
          setResults((prev) =>
            prev.map((r) =>
              r.messageId === item.messageId
                ? {
                    ...r,
                    status: "done",
                    result: {
                      matches: [],
                      supported: true,
                    },
                  }
                : r,
            ),
          );
          continue;
        }
        try {
          const data = await client.risk.rules.test({
            testDetectionRuleRequestBody: {
              ruleId,
              text: item.fullText,
              ...(matchConfig ? { matchConfig } : {}),
            },
          });
          setResults((prev) =>
            prev.map((r) =>
              r.messageId === item.messageId
                ? { ...r, status: "done", result: data }
                : r,
            ),
          );
        } catch (err) {
          const message =
            err instanceof Error ? err.message : "Failed to run rule";
          setResults((prev) =>
            prev.map((r) =>
              r.messageId === item.messageId
                ? { ...r, status: "error", errorMessage: message }
                : r,
            ),
          );
        }
      }
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to load chat");
    } finally {
      setRunning(false);
    }
  };

  if (chatsQuery.isLoading) {
    return (
      <div className="text-muted-foreground flex items-center gap-2 text-xs">
        <Loader2 className="h-3 w-3 animate-spin" />
        Loading chats…
      </div>
    );
  }
  if (byUser.length === 0) {
    return (
      <p className="text-muted-foreground text-xs">
        No chats in the last 7 days for this project.
      </p>
    );
  }

  return (
    <div className="space-y-3">
      <ChatPickerColumn
        title="External user"
        emptyLabel="No users with recent chats"
        items={byUser.map(([user, chats]) => ({
          key: user,
          label: user,
          meta: `${chats.length} chat${chats.length === 1 ? "" : "s"}`,
        }))}
        value={selectedUser}
        onChange={(next) => {
          setSelectedUser(next);
          setSelectedChat(null);
        }}
      />

      {selectedUser && (
        <ChatPickerColumn
          title="Chat"
          emptyLabel="No chats for this user"
          items={(
            byUser.find(([user]) => user === selectedUser)?.[1] ?? []
          ).map((chat) => ({
            key: chat.id,
            label: chat.title || "(untitled)",
            meta: `${chat.numMessages} msgs · ${formatTimestamp(
              chat.lastMessageTimestamp,
            )}`,
          }))}
          value={selectedChat}
          onChange={setSelectedChat}
        />
      )}

      <div className="flex items-center justify-between">
        <Button
          size="sm"
          disabled={!selectedChat || running}
          onClick={() => void handleRun()}
        >
          {running ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
          Run on chat
        </Button>
        {overflowWarning && (
          <span className="text-muted-foreground text-xs">
            {overflowWarning}
          </span>
        )}
      </div>

      {results.length > 0 && (
        <div className="border-border divide-border max-h-[420px] divide-y overflow-y-auto rounded-lg border">
          {results.map((r) => (
            <ChatMessageRow key={r.messageId} item={r} />
          ))}
        </div>
      )}
    </div>
  );
}

function ChatPickerColumn({
  title,
  emptyLabel,
  items,
  value,
  onChange,
}: {
  title: string;
  emptyLabel: string;
  items: { key: string; label: string; meta?: string }[];
  value: string | null;
  onChange: (next: string) => void;
}) {
  return (
    <div>
      <div className="text-muted-foreground mb-1 text-[11px] font-medium tracking-wide uppercase">
        {title}
      </div>
      {items.length === 0 ? (
        <p className="text-muted-foreground text-xs">{emptyLabel}</p>
      ) : (
        <RadioGroup
          value={value ?? ""}
          onValueChange={onChange}
          className="border-border divide-border max-h-48 divide-y overflow-y-auto rounded-md border"
        >
          {items.map((item) => (
            <label
              key={item.key}
              htmlFor={`pick-${title}-${item.key}`}
              className="hover:bg-muted/40 flex cursor-pointer items-center gap-3 px-3 py-2 text-xs"
            >
              <RadioGroupItem
                id={`pick-${title}-${item.key}`}
                value={item.key}
              />
              <span className="min-w-0 flex-1 truncate">{item.label}</span>
              {item.meta && (
                <span className="text-muted-foreground shrink-0 font-mono text-[10px]">
                  {item.meta}
                </span>
              )}
            </label>
          ))}
        </RadioGroup>
      )}
    </div>
  );
}

function ChatMessageRow({ item }: { item: ChatMessageResult }) {
  const [expanded, setExpanded] = useState(false);
  const matchCount = item.result?.matches.length ?? 0;
  const truncated = item.fullText.length > 240;
  return (
    <div className="px-3 py-2 text-xs">
      <button
        type="button"
        onClick={() => setExpanded((e) => !e)}
        className="flex w-full items-start gap-3 text-left"
      >
        <span className="text-muted-foreground w-6 shrink-0 text-[10px] uppercase">
          {item.role.slice(0, 4)}
        </span>
        <span className="min-w-0 flex-1">
          <span className="text-muted-foreground line-clamp-2 font-mono text-[11px]">
            {item.textPreview || "(empty)"}
          </span>
        </span>
        <span className="shrink-0">
          {item.status === "pending" && (
            <Loader2 className="h-3 w-3 animate-spin" />
          )}
          {item.status === "error" && (
            <Badge variant="destructive">error</Badge>
          )}
          {item.status === "done" &&
            (matchCount > 0 ? (
              <Badge>{matchCount}</Badge>
            ) : (
              <Badge variant="secondary">0</Badge>
            ))}
        </span>
      </button>
      {expanded && (
        <div className="mt-2 space-y-2 pl-9">
          {truncated && (
            <p className="text-muted-foreground text-[10px]">
              Full message ({item.fullText.length} chars):
            </p>
          )}
          <pre className="bg-muted/40 max-h-40 overflow-auto rounded px-2 py-1 font-mono text-[11px] whitespace-pre-wrap">
            {item.fullText || "(empty)"}
          </pre>
          {item.status === "error" && (
            <p className="text-destructive text-[11px]">{item.errorMessage}</p>
          )}
          {item.result && (
            <MatchList
              matches={item.result.matches}
              reason={
                item.result.supported
                  ? null
                  : (item.result.reason ?? "Rule not supported")
              }
            />
          )}
        </div>
      )}
    </div>
  );
}

function stringifyMessage(m: {
  content?: unknown;
  toolCalls?: string;
}): string {
  const parts: string[] = [];
  if (typeof m.content === "string") {
    parts.push(m.content);
  } else if (m.content != null) {
    try {
      parts.push(JSON.stringify(m.content));
    } catch {
      // ignore
    }
  }
  if (m.toolCalls) parts.push(m.toolCalls);
  return parts.join("\n");
}

function timestampMs(ts: Date | string | undefined | null): number {
  if (!ts) return 0;
  const d = ts instanceof Date ? ts : new Date(ts);
  const ms = d.getTime();
  return Number.isNaN(ms) ? 0 : ms;
}

function formatTimestamp(ts: Date | string | undefined | null): string {
  if (!ts) return "";
  const d = ts instanceof Date ? ts : new Date(ts);
  if (Number.isNaN(d.getTime())) return "";
  return d.toLocaleString([], {
    month: "short",
    day: "numeric",
    hour: "numeric",
    minute: "2-digit",
  });
}

function customRuleIDSuffix(ruleId: string): string {
  const trimmed = ruleId.trim();
  if (trimmed.startsWith(CUSTOM_RULE_ID_PREFIX)) {
    return trimmed.slice(CUSTOM_RULE_ID_PREFIX.length);
  }
  return trimmed;
}

function customRuleIDFromSuffix(suffix: string): string {
  return `${CUSTOM_RULE_ID_PREFIX}${suffix.trim()}`;
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
  onCreate: (rule: CustomRuleDraft) => void;
}) {
  const [step, setStep] = useState<CreateStep>("prompt");
  const [askPrompt, setAskPrompt] = useState("");
  const [idSuffix, setIdSuffix] = useState("");
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [query, setQuery] = useState("");
  const [severity, setSeverity] = useState<SeverityLevel>("medium");

  const reset = () => {
    setStep("prompt");
    setAskPrompt("");
    setIdSuffix("");
    setTitle("");
    setDescription("");
    setQuery("");
    setSeverity("medium");
  };

  const suggestMutation = useRiskSuggestCustomRuleMutation({
    onSuccess: (data) => {
      const next = data;
      setIdSuffix(customRuleIDSuffix(next.ruleId));
      setTitle(next.title);
      setDescription(next.description);
      // Prefer the suggested condition matcher (which can target tool calls,
      // arguments, keywords, …); fall back to a content/regex clause for
      // older/heuristic suggestions that only return a regex.
      if (next.matchConfig && next.matchConfig.conditions.length > 0) {
        setQuery(
          matchQueryFromConditions(
            next.matchConfig.conditions,
            next.matchConfig.combine ?? "and",
          ),
        );
      } else {
        setQuery(
          matchQueryFromConditions(
            [{ target: "content", op: "regex", value: next.regex }],
            "and",
          ),
        );
      }
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
    setIdSuffix("");
    setTitle("");
    setDescription("");
    setQuery("");
    setSeverity("medium");
    setStep("review");
  };

  const idError = useMemo(
    () =>
      idSuffix
        ? validateCustomRuleId(
            customRuleIDFromSuffix(idSuffix),
            existingCustomIds,
          )
        : null,
    [idSuffix, existingCustomIds],
  );
  const parsed = useMemo(() => parseMatchQuery(query), [query]);

  const canSubmit =
    idSuffix.trim() && title.trim() && !idError && !parsed.error;

  const handleSubmit = () => {
    const finalRuleId = customRuleIDFromSuffix(idSuffix);
    const finalIdError = validateCustomRuleId(finalRuleId, existingCustomIds);
    if (finalIdError) {
      toast.error(finalIdError);
      return;
    }
    if (parsed.error) {
      toast.error(parsed.error);
      return;
    }
    onCreate({
      id: finalRuleId,
      title: title.trim(),
      description: description.trim(),
      conditions: parsed.conditions,
      combine: parsed.combine,
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
                <div className="flex">
                  <span className="border-input bg-muted text-muted-foreground inline-flex items-center rounded-l-md border border-r-0 px-3 font-mono text-xs">
                    {CUSTOM_RULE_ID_PREFIX}
                  </span>
                  <Input
                    value={idSuffix}
                    onChange={setIdSuffix}
                    placeholder="internal_token"
                    className="rounded-l-none font-mono text-xs"
                  />
                </div>
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
                <Label className="text-sm font-medium">Match conditions</Label>
                <MatchQueryInput
                  value={query}
                  onChange={setQuery}
                  error={query.trim() ? parsed.error : null}
                />
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
