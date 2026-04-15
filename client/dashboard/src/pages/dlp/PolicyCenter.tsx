import { Page } from "@/components/page-layout";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetDescription,
  SheetFooter,
} from "@/components/ui/sheet";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { cn } from "@/lib/utils";
import { Icon, type IconName } from "@speakeasy-api/moonshine";
import { ChevronRight, Plus, Trash2 } from "lucide-react";
import { useState, useCallback, useMemo } from "react";
import { useListToolsets } from "@gram/client/react-query/index.js";
import {
  MOCK_POLICIES,
  RULE_CATEGORY_META,
  CHECK_SCOPE_META,
  DETECTION_RULES,
  createEmptyPolicy,
  type DlpPolicy,
  type RuleCategory,
  type PolicyAction,
  type CheckScope,
  type McpScope,
  type McpServerScope,
} from "./policy-data";

type McpServerInfo = {
  slug: string;
  name: string;
  tools: Array<{ name: string }>;
};

export default function PolicyCenter() {
  const [policies, setPolicies] = useState<DlpPolicy[]>(MOCK_POLICIES);
  const [editingPolicy, setEditingPolicy] = useState<DlpPolicy | null>(null);
  const [sheetOpen, setSheetOpen] = useState(false);

  const handleCreate = () => {
    setEditingPolicy(createEmptyPolicy());
    setSheetOpen(true);
  };

  const handleEdit = (policy: DlpPolicy) => {
    setEditingPolicy({ ...policy });
    setSheetOpen(true);
  };

  const handleDelete = (id: string) => {
    setPolicies((prev) => prev.filter((p) => p.id !== id));
  };

  const handleToggle = (id: string, enabled: boolean) => {
    setPolicies((prev) =>
      prev.map((p) => (p.id === id ? { ...p, enabled } : p)),
    );
  };

  const handleSave = (policy: DlpPolicy) => {
    setPolicies((prev) => {
      const existing = prev.find((p) => p.id === policy.id);
      if (existing) {
        return prev.map((p) =>
          p.id === policy.id ? { ...policy, updatedAt: new Date() } : p,
        );
      }
      return [...prev, policy];
    });
    setSheetOpen(false);
    setEditingPolicy(null);
  };

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <div className="flex items-center justify-between">
          <div>
            <h2 className="text-lg font-semibold">DLP Policies</h2>
            <p className="text-muted-foreground text-sm">
              Configure data loss prevention rules to detect and protect
              sensitive information.
            </p>
          </div>
          <Button onClick={handleCreate} size="sm">
            <Plus className="mr-1.5 size-4" />
            Create Policy
          </Button>
        </div>

        <div className="bg-card mt-4 rounded-lg border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Policy</TableHead>
                <TableHead>Rules</TableHead>
                <TableHead>Action</TableHead>
                <TableHead>Scopes</TableHead>
                <TableHead>Status</TableHead>
                <TableHead className="w-[80px]" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {policies.length === 0 && (
                <TableRow>
                  <TableCell
                    colSpan={6}
                    className="text-muted-foreground py-12 text-center"
                  >
                    No policies configured. Create your first DLP policy to get
                    started.
                  </TableCell>
                </TableRow>
              )}
              {policies.map((policy) => (
                <TableRow
                  key={policy.id}
                  className="cursor-pointer"
                  onClick={() => handleEdit(policy)}
                >
                  <TableCell>
                    <div>
                      <span className="font-medium">{policy.name}</span>
                      <div className="text-muted-foreground mt-0.5 text-xs">
                        Updated {policy.updatedAt.toLocaleDateString()}
                      </div>
                    </div>
                  </TableCell>
                  <TableCell>
                    <div className="flex flex-wrap gap-1">
                      {policy.ruleCategories.map((cat) => (
                        <Badge
                          key={cat}
                          variant="secondary"
                          className="text-xs"
                        >
                          {RULE_CATEGORY_META[cat].label}
                        </Badge>
                      ))}
                    </div>
                  </TableCell>
                  <TableCell>
                    <Badge
                      variant={
                        policy.action === "block" ? "destructive" : "outline"
                      }
                    >
                      {policy.action === "block" ? "Block" : "Flag"}
                    </Badge>
                  </TableCell>
                  <TableCell>
                    <div className="text-muted-foreground text-sm">
                      {policy.scopes.length} scope
                      {policy.scopes.length !== 1 ? "s" : ""}
                      {policy.mcpScope.mode === "selected" && (
                        <span className="ml-1">
                          &middot; {policy.mcpScope.servers.length} server
                          {policy.mcpScope.servers.length !== 1 ? "s" : ""}
                        </span>
                      )}
                    </div>
                  </TableCell>
                  <TableCell>
                    <Switch
                      checked={policy.enabled}
                      onCheckedChange={(v) => {
                        handleToggle(policy.id, v);
                      }}
                      aria-label={`Toggle ${policy.name}`}
                    />
                  </TableCell>
                  <TableCell>
                    <Button
                      variant="ghost"
                      size="icon-sm"
                      onClick={(e) => {
                        e.stopPropagation();
                        handleDelete(policy.id);
                      }}
                      tooltip="Delete policy"
                    >
                      <Trash2 className="size-3.5" />
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>

        {editingPolicy && (
          <PolicySheet
            open={sheetOpen}
            onOpenChange={(open) => {
              setSheetOpen(open);
              if (!open) setEditingPolicy(null);
            }}
            policy={editingPolicy}
            onSave={handleSave}
          />
        )}
      </Page.Body>
    </Page>
  );
}

function PolicySheet({
  open,
  onOpenChange,
  policy,
  onSave,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  policy: DlpPolicy;
  onSave: (policy: DlpPolicy) => void;
}) {
  const [draft, setDraft] = useState<DlpPolicy>(policy);
  const [expandedCategories, setExpandedCategories] = useState<
    Set<RuleCategory>
  >(new Set(policy.ruleCategories));
  const [expandedServers, setExpandedServers] = useState<Set<string>>(
    new Set(),
  );
  const [customRegex, setCustomRegex] = useState("");

  const { data: toolsets } = useListToolsets();
  const mcpServers: McpServerInfo[] = useMemo(
    () =>
      (toolsets?.toolsets ?? []).map((t) => ({
        slug: t.slug ?? "",
        name: t.name ?? t.slug ?? "",
        tools: (t.tools ?? []).map((tool) => ({
          name: tool.name ?? "",
        })),
      })),
    [toolsets],
  );

  const isEditing = MOCK_POLICIES.some((p) => p.id === policy.id);
  const isValid = draft.name.trim() !== "" && draft.selectedRules.length > 0;

  const updateDraft = useCallback(
    <K extends keyof DlpPolicy>(key: K, value: DlpPolicy[K]) => {
      setDraft((prev) => ({ ...prev, [key]: value }));
    },
    [],
  );

  const toggleCategory = (cat: RuleCategory) => {
    setExpandedCategories((prev) => {
      const next = new Set(prev);
      if (next.has(cat)) next.delete(cat);
      else next.add(cat);
      return next;
    });
  };

  const toggleCategoryAll = (cat: RuleCategory, checked: boolean) => {
    const ruleIds = DETECTION_RULES[cat].map((r) => r.id);
    setDraft((prev) => {
      const cats = checked
        ? [...new Set([...prev.ruleCategories, cat])]
        : prev.ruleCategories.filter((c) => c !== cat);
      const rules = checked
        ? [...new Set([...prev.selectedRules, ...ruleIds])]
        : prev.selectedRules.filter((id) => !ruleIds.includes(id));
      return { ...prev, ruleCategories: cats, selectedRules: rules };
    });
    if (checked) {
      setExpandedCategories((prev) => new Set([...prev, cat]));
    }
  };

  const toggleRule = (ruleId: string, cat: RuleCategory) => {
    setDraft((prev) => {
      const hasRule = prev.selectedRules.includes(ruleId);
      const rules = hasRule
        ? prev.selectedRules.filter((id) => id !== ruleId)
        : [...prev.selectedRules, ruleId];

      const catRules = DETECTION_RULES[cat].map((r) => r.id);
      const hasAnyCatRule = rules.some((id) => catRules.includes(id));
      const cats = hasAnyCatRule
        ? [...new Set([...prev.ruleCategories, cat])]
        : prev.ruleCategories.filter((c) => c !== cat);

      return { ...prev, selectedRules: rules, ruleCategories: cats };
    });
  };

  const toggleScope = (scope: CheckScope) => {
    setDraft((prev) => {
      const has = prev.scopes.includes(scope);
      return {
        ...prev,
        scopes: has
          ? prev.scopes.filter((s) => s !== scope)
          : [...prev.scopes, scope],
      };
    });
  };

  const setMcpMode = (mode: "all" | "selected") => {
    if (mode === "all") {
      updateDraft("mcpScope", { mode: "all" });
    } else {
      updateDraft("mcpScope", { mode: "selected", servers: [] });
    }
  };

  const toggleMcpServer = (slug: string) => {
    setDraft((prev) => {
      if (prev.mcpScope.mode === "all") return prev;
      const servers = prev.mcpScope.servers;
      const existing = servers.find((s) => s.slug === slug);
      const next: McpServerScope[] = existing
        ? servers.filter((s) => s.slug !== slug)
        : [...servers, { slug, allTools: true, selectedTools: [] }];
      return {
        ...prev,
        mcpScope: { mode: "selected" as const, servers: next },
      };
    });
  };

  const setServerAllTools = (slug: string, allTools: boolean) => {
    setDraft((prev) => {
      if (prev.mcpScope.mode === "all") return prev;
      return {
        ...prev,
        mcpScope: {
          mode: "selected" as const,
          servers: prev.mcpScope.servers.map((s) =>
            s.slug === slug
              ? {
                  ...s,
                  allTools,
                  selectedTools: allTools ? [] : s.selectedTools,
                }
              : s,
          ),
        },
      };
    });
  };

  const toggleServerTool = (slug: string, toolName: string) => {
    setDraft((prev) => {
      if (prev.mcpScope.mode === "all") return prev;
      return {
        ...prev,
        mcpScope: {
          mode: "selected" as const,
          servers: prev.mcpScope.servers.map((s) => {
            if (s.slug !== slug) return s;
            const has = s.selectedTools.includes(toolName);
            return {
              ...s,
              selectedTools: has
                ? s.selectedTools.filter((t) => t !== toolName)
                : [...s.selectedTools, toolName],
            };
          }),
        },
      };
    });
  };

  const toggleServerExpanded = (slug: string) => {
    setExpandedServers((prev) => {
      const next = new Set(prev);
      if (next.has(slug)) next.delete(slug);
      else next.add(slug);
      return next;
    });
  };

  const selectedServerSlugs = useMemo(() => {
    if (draft.mcpScope.mode === "all") return new Set<string>();
    return new Set(draft.mcpScope.servers.map((s) => s.slug));
  }, [draft.mcpScope]);

  const getServerScope = (slug: string): McpServerScope | undefined => {
    if (draft.mcpScope.mode === "all") return undefined;
    return draft.mcpScope.servers.find((s) => s.slug === slug);
  };

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="flex w-full flex-col sm:max-w-lg">
        <SheetHeader>
          <SheetTitle>{isEditing ? "Edit Policy" : "Create Policy"}</SheetTitle>
          <SheetDescription>
            Configure detection rules, action, and scope for this DLP policy.
          </SheetDescription>
        </SheetHeader>

        <div className="flex-1 space-y-6 overflow-y-auto px-4 pb-4">
          {/* Policy Name */}
          <div className="space-y-2">
            <Label className="text-sm font-medium">Policy Name</Label>
            <Input
              value={draft.name}
              onChange={(v) => updateDraft("name", v)}
              placeholder="e.g. Production Secret Scanner"
            />
          </div>

          {/* DLP Rules */}
          <div className="space-y-2">
            <Label className="text-sm font-medium">Detection Rules</Label>
            <p className="text-muted-foreground text-xs">
              Select which categories and individual rules to enable.
            </p>
            <div className="border-border rounded-md border">
              {(Object.keys(RULE_CATEGORY_META) as RuleCategory[]).map(
                (cat) => {
                  const meta = RULE_CATEGORY_META[cat];
                  const rules = DETECTION_RULES[cat];
                  const isExpanded = expandedCategories.has(cat);
                  const selectedInCat = rules.filter((r) =>
                    draft.selectedRules.includes(r.id),
                  );
                  const allSelected =
                    rules.length > 0 && selectedInCat.length === rules.length;
                  const someSelected = selectedInCat.length > 0 && !allSelected;

                  if (cat === "custom") {
                    return (
                      <CustomRuleSection
                        key={cat}
                        isExpanded={isExpanded}
                        onToggleExpand={() => toggleCategory(cat)}
                        customRegex={customRegex}
                        onCustomRegexChange={setCustomRegex}
                      />
                    );
                  }

                  return (
                    <div
                      key={cat}
                      className="border-border border-b last:border-b-0"
                    >
                      <div className="flex items-center gap-3 px-3 py-2.5">
                        <Checkbox
                          checked={
                            allSelected
                              ? true
                              : someSelected
                                ? "indeterminate"
                                : false
                          }
                          onCheckedChange={(checked) =>
                            toggleCategoryAll(cat, !!checked)
                          }
                        />
                        <button
                          type="button"
                          className="flex flex-1 items-center gap-2 text-left"
                          onClick={() => toggleCategory(cat)}
                        >
                          <Icon
                            name={meta.icon as IconName}
                            className="text-muted-foreground size-4"
                          />
                          <div className="min-w-0 flex-1">
                            <div className="text-sm font-medium">
                              {meta.label}
                            </div>
                            <div className="text-muted-foreground text-xs">
                              {meta.description}
                            </div>
                          </div>
                          <div className="flex items-center gap-2">
                            {selectedInCat.length > 0 && (
                              <Badge variant="secondary" className="text-xs">
                                {selectedInCat.length}/{rules.length}
                              </Badge>
                            )}
                            <ChevronRight
                              className={cn(
                                "text-muted-foreground size-4 transition-transform",
                                isExpanded && "rotate-90",
                              )}
                            />
                          </div>
                        </button>
                      </div>
                      {isExpanded && rules.length > 0 && (
                        <div className="bg-muted/30 border-border border-t">
                          {rules.map((rule) => (
                            <label
                              key={rule.id}
                              className="hover:bg-muted/50 flex cursor-pointer items-start gap-3 px-3 py-2 pl-10"
                            >
                              <Checkbox
                                checked={draft.selectedRules.includes(rule.id)}
                                onCheckedChange={() => toggleRule(rule.id, cat)}
                                className="mt-0.5"
                              />
                              <div className="min-w-0">
                                <div className="font-mono text-xs">
                                  {rule.id}
                                </div>
                                <div className="text-muted-foreground text-xs">
                                  {rule.description}
                                </div>
                              </div>
                              <Badge
                                variant="outline"
                                className="ml-auto shrink-0 text-[10px]"
                              >
                                {rule.source}
                              </Badge>
                            </label>
                          ))}
                        </div>
                      )}
                    </div>
                  );
                },
              )}
            </div>
          </div>

          {/* Action */}
          <div className="space-y-2">
            <Label className="text-sm font-medium">Action</Label>
            <RadioGroup
              value={draft.action}
              onValueChange={(v) => updateDraft("action", v as PolicyAction)}
              className="gap-2"
            >
              <div className="border-border flex items-start gap-3 rounded-md border p-3">
                <RadioGroupItem
                  value="flag"
                  id="action-flag"
                  className="mt-0.5"
                />
                <div>
                  <Label
                    htmlFor="action-flag"
                    className="cursor-pointer font-medium"
                  >
                    Flag
                  </Label>
                  <p className="text-muted-foreground text-xs">
                    Log the event and surface it in the DLP dashboard for
                    review.
                  </p>
                </div>
              </div>
              <div className="border-border flex items-start gap-3 rounded-md border p-3">
                <RadioGroupItem
                  value="block"
                  id="action-block"
                  className="mt-0.5"
                />
                <div>
                  <Label
                    htmlFor="action-block"
                    className="cursor-pointer font-medium"
                  >
                    Block
                  </Label>
                  <p className="text-muted-foreground text-xs">
                    Prevent the message or tool call from being processed and
                    notify the user.
                  </p>
                </div>
              </div>
            </RadioGroup>
          </div>

          {/* Check Scopes */}
          <div className="space-y-3">
            <div>
              <Label className="text-sm font-medium">Check Scope</Label>
              <p className="text-muted-foreground text-xs">
                Select which message and tool types this policy applies to.
              </p>
            </div>

            {/* Message / tool type scopes */}
            <div className="border-border space-y-0 rounded-md border">
              {(Object.keys(CHECK_SCOPE_META) as CheckScope[]).map((scope) => {
                const meta = CHECK_SCOPE_META[scope];
                return (
                  <label
                    key={scope}
                    className="hover:bg-muted/50 border-border flex cursor-pointer items-center gap-3 border-b px-3 py-2.5 last:border-b-0"
                  >
                    <Checkbox
                      checked={draft.scopes.includes(scope)}
                      onCheckedChange={() => toggleScope(scope)}
                    />
                    <div className="min-w-0">
                      <div className="text-sm font-medium">{meta.label}</div>
                      <div className="text-muted-foreground text-xs">
                        {meta.description}
                      </div>
                    </div>
                  </label>
                );
              })}
            </div>

            {/* MCP Server scoping */}
            <div>
              <Label className="text-sm font-medium">MCP Server Scope</Label>
              <p className="text-muted-foreground mb-2 text-xs">
                Choose whether this policy applies to all MCP servers or only
                specific ones. For selected servers, you can further limit to
                specific tools.
              </p>

              <RadioGroup
                value={draft.mcpScope.mode}
                onValueChange={(v) => setMcpMode(v as "all" | "selected")}
                className="gap-2"
              >
                <div className="border-border flex items-center gap-3 rounded-md border p-3">
                  <RadioGroupItem value="all" id="mcp-all" />
                  <Label htmlFor="mcp-all" className="cursor-pointer text-sm">
                    All MCP Servers
                  </Label>
                </div>
                <div className="border-border rounded-md border">
                  <div className="flex items-center gap-3 p-3">
                    <RadioGroupItem value="selected" id="mcp-selected" />
                    <Label
                      htmlFor="mcp-selected"
                      className="cursor-pointer text-sm"
                    >
                      Selected MCP Servers
                    </Label>
                  </div>

                  {draft.mcpScope.mode === "selected" && (
                    <div className="border-border border-t">
                      {mcpServers.length === 0 && (
                        <div className="text-muted-foreground px-3 py-4 text-center text-xs">
                          No MCP servers configured in this project.
                        </div>
                      )}
                      {mcpServers.map((server) => {
                        const isServerSelected = selectedServerSlugs.has(
                          server.slug,
                        );
                        const serverScope = getServerScope(server.slug);
                        const isServerExpanded = expandedServers.has(
                          server.slug,
                        );
                        const hasTools = server.tools.length > 0;

                        return (
                          <div
                            key={server.slug}
                            className="border-border border-b last:border-b-0"
                          >
                            {/* Server row */}
                            <div className="flex items-center gap-3 px-3 py-2.5">
                              <Checkbox
                                checked={isServerSelected}
                                onCheckedChange={() =>
                                  toggleMcpServer(server.slug)
                                }
                              />
                              <button
                                type="button"
                                className="flex flex-1 items-center gap-2 text-left"
                                onClick={() => {
                                  if (!isServerSelected) {
                                    toggleMcpServer(server.slug);
                                  }
                                  if (hasTools) {
                                    toggleServerExpanded(server.slug);
                                  }
                                }}
                              >
                                <Icon
                                  name="network"
                                  className="text-muted-foreground size-4"
                                />
                                <span className="flex-1 text-sm font-medium">
                                  {server.name}
                                </span>
                                {isServerSelected && hasTools && (
                                  <div className="flex items-center gap-2">
                                    <Badge
                                      variant="secondary"
                                      className="text-xs"
                                    >
                                      {serverScope?.allTools
                                        ? "All tools"
                                        : `${serverScope?.selectedTools.length ?? 0}/${server.tools.length} tools`}
                                    </Badge>
                                    <ChevronRight
                                      className={cn(
                                        "text-muted-foreground size-4 transition-transform",
                                        isServerExpanded && "rotate-90",
                                      )}
                                    />
                                  </div>
                                )}
                              </button>
                            </div>

                            {/* Tool-level scoping */}
                            {isServerSelected &&
                              isServerExpanded &&
                              hasTools && (
                                <div className="bg-muted/30 border-border border-t">
                                  {/* All tools toggle */}
                                  <label className="hover:bg-muted/50 flex cursor-pointer items-center gap-3 px-3 py-2 pl-10">
                                    <Checkbox
                                      checked={serverScope?.allTools ?? true}
                                      onCheckedChange={(checked) =>
                                        setServerAllTools(
                                          server.slug,
                                          !!checked,
                                        )
                                      }
                                    />
                                    <span className="text-sm font-medium">
                                      All tools
                                    </span>
                                  </label>

                                  {/* Individual tools */}
                                  {!serverScope?.allTools &&
                                    server.tools.map((tool) => (
                                      <label
                                        key={tool.name}
                                        className="hover:bg-muted/50 flex cursor-pointer items-center gap-3 px-3 py-2 pl-10"
                                      >
                                        <Checkbox
                                          checked={
                                            serverScope?.selectedTools.includes(
                                              tool.name,
                                            ) ?? false
                                          }
                                          onCheckedChange={() =>
                                            toggleServerTool(
                                              server.slug,
                                              tool.name,
                                            )
                                          }
                                        />
                                        <span className="font-mono text-xs">
                                          {tool.name}
                                        </span>
                                      </label>
                                    ))}
                                </div>
                              )}
                          </div>
                        );
                      })}
                    </div>
                  )}
                </div>
              </RadioGroup>
            </div>
          </div>
        </div>

        <SheetFooter className="border-border border-t">
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button onClick={() => onSave(draft)} disabled={!isValid}>
            {isEditing ? "Save Changes" : "Create Policy"}
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  );
}

function CustomRuleSection({
  isExpanded,
  onToggleExpand,
  customRegex,
  onCustomRegexChange,
}: {
  isExpanded: boolean;
  onToggleExpand: () => void;
  customRegex: string;
  onCustomRegexChange: (v: string) => void;
}) {
  const meta = RULE_CATEGORY_META.custom;

  return (
    <div className="border-border border-b last:border-b-0">
      <button
        type="button"
        className="flex w-full items-center gap-3 px-3 py-2.5 text-left"
        onClick={onToggleExpand}
      >
        <div className="size-4" />
        <Icon
          name={meta.icon as IconName}
          className="text-muted-foreground size-4"
        />
        <div className="min-w-0 flex-1">
          <div className="text-sm font-medium">{meta.label}</div>
          <div className="text-muted-foreground text-xs">
            {meta.description}
          </div>
        </div>
        <ChevronRight
          className={cn(
            "text-muted-foreground size-4 transition-transform",
            isExpanded && "rotate-90",
          )}
        />
      </button>
      {isExpanded && (
        <div className="bg-muted/30 border-border space-y-3 border-t px-3 py-3 pl-10">
          <div>
            <Label className="text-xs font-medium">Custom Regex Pattern</Label>
            <Input
              value={customRegex}
              onChange={onCustomRegexChange}
              placeholder="e.g. INTERNAL-\d{6}"
              className="mt-1 font-mono text-xs"
            />
            <p className="text-muted-foreground mt-1 text-xs">
              Enter a regular expression to match organization-specific
              sensitive data patterns.
            </p>
          </div>
        </div>
      )}
    </div>
  );
}
