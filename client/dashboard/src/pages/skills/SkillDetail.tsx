import { FileBrowser } from "@/components/file-browser/FileBrowser";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  PageTabsTrigger,
  Tabs,
  TabsContent,
  TabsList,
} from "@/components/ui/tabs";
import { Type } from "@/components/ui/type";
import { cn } from "@/lib/utils";
import { useRoutes } from "@/routes";
import { Icon } from "@speakeasy-api/moonshine";
import { CheckCircleIcon, ClipboardCopyIcon, XCircleIcon } from "lucide-react";
import { useCallback, useMemo, useState } from "react";
import { useParams } from "react-router";
import {
  type RegistrySkill,
  type SkillDigest,
  type SkillTag,
  type SkillVisibility,
  formatDate,
  formatTime,
  findFolderByPath,
  MOCK_ALL_ROLES,
  MOCK_CONTEXT_TREE,
  MOCK_REGISTRY_SKILLS,
  MOCK_SKILL_INVOCATIONS,
} from "../context/mock-data";

export default function SkillDetail() {
  const { skillId } = useParams<{ skillId: string }>();
  const routes = useRoutes();
  const skill = MOCK_REGISTRY_SKILLS.find((s) => s.id === skillId);

  if (!skill) {
    return (
      <div className="flex flex-col items-center justify-center py-24">
        <Type variant="subheading" className="text-muted-foreground">
          Skill not found
        </Type>
        <Button
          variant="outline"
          className="mt-4"
          onClick={() => routes.skills.goTo()}
        >
          Back to Skills
        </Button>
      </div>
    );
  }

  return <SkillDetailInner skill={skill} />;
}

function SkillDetailInner({ skill }: { skill: RegistrySkill }) {
  const latestDigest = skill.digests[0];
  const audit = latestDigest?.audit;
  const invocations = useMemo(
    () => MOCK_SKILL_INVOCATIONS.filter((i) => i.skillId === skill.id),
    [skill.id],
  );
  const filesFolder = useMemo(
    () => (skill.path ? findFolderByPath(MOCK_CONTEXT_TREE, skill.path) : null),
    [skill.path],
  );

  return (
    <div className="flex flex-col h-full">
      {/* Header — always visible above tabs */}
      <div className="px-8 pt-6 pb-4">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-3">
            <Icon name="sparkles" className="h-5 w-5 text-primary" />
            <Type variant="subheading" className="text-lg">
              {skill.name}
            </Type>
            <SkillStatusBadge status={skill.status} />
          </div>
          <Button size="sm" variant="ghost">
            <Icon name="download" className="h-4 w-4" />
          </Button>
        </div>
        <Type small muted className="block mt-1">
          {skill.description}
        </Type>
        <div className="flex items-center gap-4 mt-2 text-xs text-muted-foreground">
          {skill.path && <span className="font-mono">{skill.path}</span>}
          <span>by {skill.author}</span>
          <span>Updated {formatDate(skill.updatedAt)}</span>
          {skill.capturedFrom && (
            <span>
              Captured from{" "}
              <span className="font-medium text-foreground">
                {skill.capturedFrom.agentName}
              </span>
            </span>
          )}
        </div>
      </div>

      {/* Tabs */}
      <Tabs defaultValue="skill-md" className="flex flex-col flex-1 min-h-0">
        <div className="border-b">
          <div className="px-8">
            <TabsList className="h-auto bg-transparent p-0 gap-6 rounded-none items-stretch">
              <PageTabsTrigger value="skill-md">SKILL.md</PageTabsTrigger>
              {filesFolder && (
                <PageTabsTrigger value="files">Files</PageTabsTrigger>
              )}
              <PageTabsTrigger value="versions">
                Versions
                <span className="ml-1 text-muted-foreground">
                  {skill.digests.length}
                </span>
              </PageTabsTrigger>
              <PageTabsTrigger value="security">
                Security
                {audit && (
                  <Badge
                    variant="outline"
                    className={cn(
                      "ml-1.5 text-[10px] uppercase py-0 h-4",
                      RISK_COLORS[audit.riskLevel],
                    )}
                  >
                    {audit.riskLevel}
                  </Badge>
                )}
              </PageTabsTrigger>
              <PageTabsTrigger value="install">Install</PageTabsTrigger>
              <PageTabsTrigger value="activity">
                Activity
                {invocations.length > 0 && (
                  <span className="ml-1 text-muted-foreground">
                    {invocations.length}
                  </span>
                )}
              </PageTabsTrigger>
              <PageTabsTrigger value="settings">Settings</PageTabsTrigger>
            </TabsList>
          </div>
        </div>

        <TabsContent
          value="skill-md"
          className="flex-1 min-h-0 p-8 overflow-y-auto"
        >
          <SkillBodyTab skill={skill} />
        </TabsContent>
        {filesFolder && (
          <TabsContent value="files" className="flex-1 min-h-0">
            <FileBrowser
              root={filesFolder}
              label={skill.name}
              hideAddButton
              compact
            />
          </TabsContent>
        )}
        <TabsContent
          value="versions"
          className="flex-1 min-h-0 p-8 overflow-y-auto"
        >
          <VersionsTab digests={skill.digests} tags={skill.tags} />
        </TabsContent>
        <TabsContent
          value="security"
          className="flex-1 min-h-0 p-8 overflow-y-auto"
        >
          <SecurityTab digest={latestDigest} />
        </TabsContent>
        <TabsContent
          value="install"
          className="flex-1 min-h-0 p-8 overflow-y-auto"
        >
          <InstallTab skill={skill} />
        </TabsContent>
        <TabsContent
          value="activity"
          className="flex-1 min-h-0 p-8 overflow-y-auto"
        >
          <ActivityTab skillId={skill.id} />
        </TabsContent>
        <TabsContent
          value="settings"
          className="flex-1 min-h-0 p-8 overflow-y-auto"
        >
          <SettingsTab skill={skill} />
        </TabsContent>
      </Tabs>
    </div>
  );
}

// ── SKILL.md Tab ─────────────────────────────────────────────────────────────

function SkillBodyTab({ skill }: { skill: RegistrySkill }) {
  return (
    <div className="max-w-3xl space-y-4">
      {Object.keys(skill.frontmatter).length > 0 && (
        <div className="flex flex-wrap gap-1.5">
          {Object.entries(skill.frontmatter).map(([key, value]) => (
            <Badge key={key} variant="secondary">
              {key}: {value}
            </Badge>
          ))}
        </div>
      )}
      <pre className="text-sm font-mono whitespace-pre-wrap text-foreground bg-muted/30 rounded-md p-4 max-h-[600px] overflow-auto">
        {skill.body}
      </pre>
    </div>
  );
}

// ── Versions Tab ─────────────────────────────────────────────────────────────

function VersionsTab({
  digests,
  tags,
}: {
  digests: SkillDigest[];
  tags: SkillTag[];
}) {
  const tagsByHash = useMemo(() => {
    const map: Record<string, SkillTag[]> = {};
    for (const t of tags) {
      (map[t.contentHash] ??= []).push(t);
    }
    return map;
  }, [tags]);

  const latestHash = tags.find((t) => t.tag === "latest")?.contentHash;

  return (
    <div className="max-w-4xl">
      <div className="rounded-lg border border-border bg-card overflow-hidden">
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border bg-muted/30">
                <th className="px-4 py-2.5 text-left font-medium text-muted-foreground">
                  Digest
                </th>
                <th className="px-4 py-2.5 text-left font-medium text-muted-foreground">
                  Tags
                </th>
                <th className="px-4 py-2.5 text-left font-medium text-muted-foreground">
                  Pushed
                </th>
                <th className="px-4 py-2.5 text-left font-medium text-muted-foreground">
                  By
                </th>
                <th className="px-4 py-2.5 text-left font-medium text-muted-foreground">
                  Origin
                </th>
                <th className="px-4 py-2.5 text-right font-medium text-muted-foreground">
                  Size
                </th>
                <th className="px-4 py-2.5 text-left font-medium text-muted-foreground">
                  Message
                </th>
              </tr>
            </thead>
            <tbody>
              {digests.map((d) => {
                const digestTags = tagsByHash[d.contentHash] ?? [];
                return (
                  <tr
                    key={d.contentHash}
                    className="border-b border-border last:border-b-0"
                  >
                    <td className="px-4 py-2.5">
                      <code className="text-xs font-mono text-muted-foreground">
                        {d.contentHash.slice(0, 19)}
                      </code>
                    </td>
                    <td className="px-4 py-2.5">
                      <div className="flex flex-wrap gap-1">
                        {digestTags.map((t) => (
                          <Badge
                            key={t.tag}
                            variant="outline"
                            className={cn(
                              "text-[10px] font-mono",
                              t.tag === "latest"
                                ? "border-primary/50 text-primary bg-primary/10"
                                : "border-border",
                            )}
                          >
                            {t.tag}
                          </Badge>
                        ))}
                        {digestTags.length === 0 && (
                          <span className="text-xs text-muted-foreground/50">
                            untagged
                          </span>
                        )}
                      </div>
                    </td>
                    <td className="px-4 py-2.5 text-muted-foreground">
                      {formatDate(d.pushedAt)}
                    </td>
                    <td className="px-4 py-2.5 text-muted-foreground">
                      {d.pushedBy}
                    </td>
                    <td className="px-4 py-2.5">
                      <OriginBadge provenance={d.provenance} />
                    </td>
                    <td className="px-4 py-2.5 text-right tabular-nums text-muted-foreground">
                      {d.bodyBytes.toLocaleString()} B
                    </td>
                    <td className="px-4 py-2.5 text-muted-foreground max-w-[200px] truncate">
                      {d.message ?? "\u2014"}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  );
}

// ── Security Tab ─────────────────────────────────────────────────────────────

const RISK_COLORS: Record<string, string> = {
  safe: "border-emerald-500/50 text-emerald-600 bg-emerald-500/10",
  caution: "border-amber-500/50 text-amber-600 bg-amber-500/10",
  warning: "border-orange-500/50 text-orange-600 bg-orange-500/10",
  critical: "border-destructive/50 text-destructive bg-destructive/10",
};

const CHECK_STATUS_COLORS: Record<string, string> = {
  pass: "text-emerald-600",
  info: "text-primary",
  warn: "text-amber-600",
  fail: "text-destructive",
};

const CHECK_STATUS_ICON: Record<string, string> = {
  pass: "check-circle",
  info: "info",
  warn: "alert-triangle",
  fail: "x-circle",
};

function SecurityTab({ digest }: { digest: SkillDigest }) {
  const audit = digest.audit;

  if (!audit) {
    return (
      <div className="max-w-3xl rounded-lg border border-dashed border-border bg-card px-4 py-12 text-center">
        <Type small muted>
          No security audit available for this version.
        </Type>
      </div>
    );
  }

  return (
    <div className="max-w-3xl space-y-4">
      {/* Summary */}
      <div className="flex items-center gap-3">
        <Badge
          variant="outline"
          className={cn("text-xs uppercase", RISK_COLORS[audit.riskLevel])}
        >
          {audit.riskLevel}
        </Badge>
        <Type small muted>
          Analyzed {formatDate(audit.analyzedAt)} &middot;{" "}
          <code className="font-mono text-[10px]">
            {audit.contentHash.slice(0, 19)}
          </code>
        </Type>
      </div>

      {/* Checks */}
      <div className="rounded-lg border border-border bg-card overflow-hidden divide-y divide-border">
        {audit.checks.map((check, i) => (
          <div key={i} className="flex items-start gap-3 px-4 py-2.5">
            <Icon
              name={CHECK_STATUS_ICON[check.status] as any}
              className={cn(
                "h-4 w-4 mt-0.5 shrink-0",
                CHECK_STATUS_COLORS[check.status],
              )}
            />
            <div className="min-w-0">
              <div className="flex items-center gap-2">
                <Type small className="font-medium">
                  {check.label}
                </Type>
                <Badge variant="secondary" className="text-[10px]">
                  {check.category}
                </Badge>
              </div>
              <Type small muted className="block mt-0.5">
                {check.detail}
              </Type>
            </div>
          </div>
        ))}
      </div>

      {/* Full analysis */}
      <div className="rounded-lg border border-border bg-muted/20 px-4 py-3">
        <Type small className="font-medium block mb-1.5">
          Full Analysis
        </Type>
        <Type small muted className="block leading-relaxed">
          {audit.analysis}
        </Type>
      </div>
    </div>
  );
}

// ── Install Tab ──────────────────────────────────────────────────────────────

type AgentId = "claude-code" | "cursor" | "windsurf" | "copilot" | "codex";

const AGENT_LABELS: Record<AgentId, string> = {
  "claude-code": "Claude Code",
  cursor: "Cursor",
  windsurf: "Windsurf",
  copilot: "GitHub Copilot",
  codex: "Codex",
};

function getInstallSnippets(
  skill: RegistrySkill,
): Record<AgentId, { file: string; content: string }> {
  const name = skill.name;
  const desc = skill.description;
  const body = skill.body;

  return {
    "claude-code": {
      file: `.claude/commands/${name}.md`,
      content: `---
description: ${desc}
---

${body}`,
    },
    cursor: {
      file: `.cursor/rules/${name}.mdc`,
      content: `---
description: ${desc}
globs: "**/*"
alwaysApply: false
---

${body}`,
    },
    windsurf: {
      file: `.windsurfrules`,
      content: `# ${name}
# ${desc}

${body}`,
    },
    copilot: {
      file: `.github/copilot-instructions.md`,
      content: `<!-- ${name}: ${desc} -->

${body}`,
    },
    codex: {
      file: `AGENTS.md`,
      content: `<!-- ${name}: ${desc} -->

${body}`,
    },
  };
}

function InstallTab({ skill }: { skill: RegistrySkill }) {
  const [selectedAgent, setSelectedAgent] = useState<AgentId>("claude-code");
  const [copied, setCopied] = useState(false);
  const snippets = useMemo(() => getInstallSnippets(skill), [skill]);
  const snippet = snippets[selectedAgent];

  const handleCopy = useCallback(() => {
    navigator.clipboard.writeText(snippet.content);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  }, [snippet.content]);

  return (
    <div className="max-w-3xl space-y-4">
      {/* Agent picker */}
      <div className="flex items-center gap-1 rounded-lg border border-border bg-card p-1 w-fit">
        {(Object.keys(AGENT_LABELS) as AgentId[]).map((id) => (
          <button
            key={id}
            onClick={() => setSelectedAgent(id)}
            className={cn(
              "px-3 py-1.5 text-xs font-medium rounded-md transition-colors",
              selectedAgent === id
                ? "bg-foreground text-background"
                : "text-muted-foreground hover:text-foreground hover:bg-muted/50",
            )}
          >
            {AGENT_LABELS[id]}
          </button>
        ))}
      </div>

      {/* File path */}
      <div className="rounded-lg border border-border bg-card overflow-hidden">
        <div className="flex items-center justify-between px-4 py-2.5 border-b border-border bg-muted/30">
          <code className="text-xs font-mono text-muted-foreground">
            {snippet.file}
          </code>
          <Button
            size="sm"
            variant="ghost"
            className="h-6 px-2 text-xs gap-1"
            onClick={handleCopy}
          >
            <ClipboardCopyIcon className="h-3 w-3" />
            {copied ? "Copied" : "Copy"}
          </Button>
        </div>
        <pre className="p-4 text-sm font-mono whitespace-pre-wrap text-foreground overflow-auto max-h-[400px]">
          {snippet.content}
        </pre>
      </div>

      <Type small muted className="block">
        Create this file in your project to use{" "}
        <span className="font-medium text-foreground">{skill.name}</span> with{" "}
        {AGENT_LABELS[selectedAgent]}.
      </Type>
    </div>
  );
}

// ── Activity Tab ─────────────────────────────────────────────────────────────

function ActivityTab({ skillId }: { skillId: string }) {
  const invocations = useMemo(
    () => MOCK_SKILL_INVOCATIONS.filter((i) => i.skillId === skillId),
    [skillId],
  );

  if (invocations.length === 0) {
    return (
      <div className="max-w-3xl rounded-lg border border-dashed border-border bg-card p-12 text-center">
        <Type small muted>
          No invocations recorded for this skill.
        </Type>
      </div>
    );
  }

  return (
    <div className="max-w-4xl">
      <div className="rounded-lg border border-border bg-card overflow-hidden">
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border bg-muted/30">
                <th className="px-4 py-2.5 text-left font-medium text-muted-foreground">
                  Time
                </th>
                <th className="px-4 py-2.5 text-left font-medium text-muted-foreground">
                  Agent
                </th>
                <th className="px-4 py-2.5 text-left font-medium text-muted-foreground">
                  Session
                </th>
                <th className="px-4 py-2.5 text-left font-medium text-muted-foreground">
                  Latency
                </th>
                <th className="px-4 py-2.5 text-left font-medium text-muted-foreground">
                  Status
                </th>
              </tr>
            </thead>
            <tbody>
              {invocations.map((inv) => (
                <tr
                  key={inv.id}
                  className="border-b border-border last:border-b-0"
                >
                  <td className="px-4 py-2.5 text-muted-foreground whitespace-nowrap">
                    {formatTime(inv.timestamp)}
                  </td>
                  <td className="px-4 py-2.5">{inv.agentName}</td>
                  <td className="px-4 py-2.5 font-mono text-xs text-muted-foreground">
                    {inv.sessionId.slice(0, 8)}...
                  </td>
                  <td className="px-4 py-2.5">
                    <LatencyBadge ms={inv.latencyMs} />
                  </td>
                  <td className="px-4 py-2.5">
                    {inv.success ? (
                      <CheckCircleIcon className="h-4 w-4 text-emerald-500" />
                    ) : (
                      <XCircleIcon className="h-4 w-4 text-destructive" />
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  );
}

// ── Settings Tab ─────────────────────────────────────────────────────────────

function SettingsTab({ skill }: { skill: RegistrySkill }) {
  const [status, setStatus] = useState(skill.status);
  const [name, setName] = useState(skill.name);
  const [visibility, setVisibility] = useState<SkillVisibility>(
    skill.visibility ?? { mode: "all" },
  );

  const isDirty =
    status !== skill.status ||
    name !== skill.name ||
    JSON.stringify(visibility) !==
      JSON.stringify(skill.visibility ?? { mode: "all" });

  const handleSave = () => {
    console.log("Saving skill", skill.id, { status, name, visibility });
  };

  const handleDiscard = () => {
    setStatus(skill.status);
    setName(skill.name);
    setVisibility(skill.visibility ?? { mode: "all" });
  };

  return (
    <div className="max-w-2xl space-y-6">
      {/* Status */}
      <SettingsSection
        title="Status"
        description="Enable or disable this skill."
      >
        <div className="flex items-center gap-2">
          <SkillStatusBadge status={status} />
          {status === "active" && (
            <Button
              size="sm"
              variant="outline"
              onClick={() => setStatus("disabled")}
            >
              Disable
            </Button>
          )}
          {status === "disabled" && (
            <Button
              size="sm"
              variant="outline"
              onClick={() => setStatus("active")}
            >
              Enable
            </Button>
          )}
          {status === "pending-review" && (
            <>
              <Button size="sm" onClick={() => setStatus("active")}>
                Approve
              </Button>
              <Button
                size="sm"
                variant="outline"
                onClick={() => setStatus("disabled")}
              >
                Reject
              </Button>
            </>
          )}
        </div>
      </SettingsSection>

      {/* Rename */}
      <SettingsSection title="Name" description="Rename this skill.">
        <input
          type="text"
          value={name}
          onChange={(e) => setName(e.target.value)}
          className="w-full max-w-sm rounded-md border border-border bg-muted/30 px-3 py-1.5 text-sm font-mono focus:outline-none focus:ring-2 focus:ring-ring"
        />
      </SettingsSection>

      {/* Visibility */}
      <VisibilitySection visibility={visibility} onChange={setVisibility} />

      {/* Upload new version */}
      <SettingsSection
        title="Upload New Version"
        description="Paste updated SKILL.md content to push a new digest."
      >
        <textarea
          placeholder={`---\nname: ${skill.name}\ndescription: ${skill.description}\n---\n\nSkill body here...`}
          className="w-full h-32 rounded-md border border-border bg-muted/30 px-3 py-2 text-sm font-mono resize-none focus:outline-none focus:ring-2 focus:ring-ring"
        />
        <Button size="sm" className="mt-2">
          Push Version
        </Button>
      </SettingsSection>

      {/* Danger zone */}
      <div className="rounded-lg border border-destructive/30 bg-card overflow-hidden">
        <div className="px-4 py-3 border-b border-destructive/20">
          <Type variant="subheading" className="text-destructive">
            Danger Zone
          </Type>
        </div>
        <div className="px-4 py-3 flex items-center justify-between">
          <div>
            <Type small className="font-medium block">
              Delete skill
            </Type>
            <Type small muted className="block">
              Permanently remove this skill and all its versions.
            </Type>
          </div>
          <Button size="sm" variant="destructive">
            Delete
          </Button>
        </div>
      </div>

      {/* Sticky save bar */}
      {isDirty && (
        <div className="sticky bottom-0 left-0 right-0 border-t border-border bg-card/95 backdrop-blur-sm px-4 py-3 -mx-8 mt-6 flex items-center justify-between">
          <Type small muted>
            You have unsaved changes
          </Type>
          <div className="flex items-center gap-2">
            <Button size="sm" variant="outline" onClick={handleDiscard}>
              Discard
            </Button>
            <Button size="sm" onClick={handleSave}>
              Save changes
            </Button>
          </div>
        </div>
      )}
    </div>
  );
}

function SettingsSection({
  title,
  description,
  children,
}: {
  title: string;
  description: string;
  children: React.ReactNode;
}) {
  return (
    <div className="rounded-lg border border-border bg-card overflow-hidden">
      <div className="px-4 py-3 border-b border-border">
        <Type variant="subheading">{title}</Type>
        <Type small muted className="mt-0.5 block">
          {description}
        </Type>
      </div>
      <div className="px-4 py-3">{children}</div>
    </div>
  );
}

// ── Visibility Section ───────────────────────────────────────────────────────

type RoleOverride = "default" | "allow" | "deny";

function deriveFromVisibility(vis: SkillVisibility) {
  let defaultPolicy: "allow" | "deny" = "allow";
  if (vis.mode === "deny") defaultPolicy = "allow";
  else if (vis.mode === "allow") defaultPolicy = "deny";
  else if (vis.mode === "none") defaultPolicy = "deny";

  const overrides: Record<string, RoleOverride> = {};
  for (const role of MOCK_ALL_ROLES) {
    if (vis.mode === "allow" && vis.roles.includes(role))
      overrides[role] = "allow";
    else if (vis.mode === "deny" && vis.roles.includes(role))
      overrides[role] = "deny";
    else overrides[role] = "default";
  }
  return { defaultPolicy, overrides };
}

function toVisibility(
  defaultPolicy: "allow" | "deny",
  overrides: Record<string, RoleOverride>,
): SkillVisibility {
  const access: Record<string, boolean> = {};
  for (const role of MOCK_ALL_ROLES) {
    const ov = overrides[role] ?? "default";
    if (ov === "allow") access[role] = true;
    else if (ov === "deny") access[role] = false;
    else access[role] = defaultPolicy === "allow";
  }
  const allowed = MOCK_ALL_ROLES.filter((r) => access[r]);
  const denied = MOCK_ALL_ROLES.filter((r) => !access[r]);
  if (allowed.length === MOCK_ALL_ROLES.length) return { mode: "all" };
  if (denied.length === MOCK_ALL_ROLES.length) return { mode: "none" };
  if (allowed.length <= denied.length)
    return { mode: "allow", roles: [...allowed] };
  return { mode: "deny", roles: [...denied] };
}

function VisibilitySection({
  visibility,
  onChange,
}: {
  visibility: SkillVisibility;
  onChange: (v: SkillVisibility) => void;
}) {
  const { defaultPolicy: initDefault, overrides: initOverrides } =
    deriveFromVisibility(visibility);
  const [defaultPolicy, setDefaultPolicyRaw] = useState<"allow" | "deny">(
    initDefault,
  );
  const [overrides, setOverridesRaw] =
    useState<Record<string, RoleOverride>>(initOverrides);

  const emitChange = (
    dp: "allow" | "deny",
    ov: Record<string, RoleOverride>,
  ) => {
    onChange(toVisibility(dp, ov));
  };

  const setDefaultPolicy = (dp: "allow" | "deny") => {
    setDefaultPolicyRaw(dp);
    emitChange(dp, overrides);
  };

  const cycleOverride = (role: string) => {
    const order: RoleOverride[] = ["default", "allow", "deny"];
    const current = overrides[role] ?? "default";
    const next = order[(order.indexOf(current) + 1) % order.length];
    const updated = { ...overrides, [role]: next };
    setOverridesRaw(updated);
    emitChange(defaultPolicy, updated);
  };

  const access: Record<string, boolean> = {};
  for (const role of MOCK_ALL_ROLES) {
    const ov = overrides[role] ?? "default";
    if (ov === "allow") access[role] = true;
    else if (ov === "deny") access[role] = false;
    else access[role] = defaultPolicy === "allow";
  }

  const allowedCount = Object.values(access).filter(Boolean).length;

  return (
    <div className="rounded-lg border border-border bg-card overflow-hidden">
      <div className="flex items-center justify-between px-4 py-3 border-b border-border">
        <div>
          <Type variant="subheading">Visibility</Type>
          <Type small muted className="mt-0.5 block">
            {allowedCount} of {MOCK_ALL_ROLES.length} roles can invoke
          </Type>
        </div>
        <div className="flex items-center gap-2">
          <Type small muted>
            Default:
          </Type>
          <div className="inline-flex items-center gap-0.5 rounded-md border border-border p-0.5">
            <button
              onClick={() => setDefaultPolicy("allow")}
              className={cn(
                "px-2.5 py-1 text-xs font-medium rounded transition-colors",
                defaultPolicy === "allow"
                  ? "bg-emerald-500/15 text-emerald-600"
                  : "text-muted-foreground hover:text-foreground",
              )}
            >
              Allow all
            </button>
            <button
              onClick={() => setDefaultPolicy("deny")}
              className={cn(
                "px-2.5 py-1 text-xs font-medium rounded transition-colors",
                defaultPolicy === "deny"
                  ? "bg-destructive/15 text-destructive"
                  : "text-muted-foreground hover:text-foreground",
              )}
            >
              Deny all
            </button>
          </div>
        </div>
      </div>

      {MOCK_ALL_ROLES.map((role) => {
        const ov = overrides[role] ?? "default";
        const hasAccess = access[role];
        const isOverridden = ov !== "default";

        return (
          <div
            key={role}
            className="flex items-center justify-between px-4 py-2.5 border-b border-border last:border-b-0"
          >
            <div className="flex items-center gap-3">
              <div
                className={cn(
                  "h-2 w-2 rounded-full shrink-0",
                  hasAccess ? "bg-emerald-500" : "bg-muted-foreground/30",
                )}
              />
              <Type small className="font-medium">
                {role}
              </Type>
              <Badge
                variant="outline"
                className={cn(
                  "text-[10px]",
                  hasAccess &&
                    !isOverridden &&
                    "border-emerald-500/50 text-emerald-600 bg-emerald-500/10",
                  hasAccess &&
                    isOverridden &&
                    "border-emerald-500/50 text-emerald-600 bg-emerald-500/10 border-dashed",
                  !hasAccess &&
                    !isOverridden &&
                    "border-muted-foreground/30 text-muted-foreground bg-muted/30",
                  !hasAccess &&
                    isOverridden &&
                    "border-destructive/50 text-destructive bg-destructive/10 border-dashed",
                )}
              >
                {hasAccess && !isOverridden && "Visible"}
                {hasAccess && isOverridden && "Visible (override)"}
                {!hasAccess && !isOverridden && "Hidden"}
                {!hasAccess && isOverridden && "Hidden (override)"}
              </Badge>
            </div>
            <button
              onClick={() => cycleOverride(role)}
              className={cn(
                "shrink-0 px-2.5 py-1 rounded-md border text-xs font-medium transition-colors",
                ov === "default" &&
                  "border-border text-muted-foreground hover:border-foreground/30",
                ov === "allow" &&
                  "border-emerald-500/50 text-emerald-600 bg-emerald-500/10",
                ov === "deny" &&
                  "border-destructive/50 text-destructive bg-destructive/10",
              )}
            >
              {ov === "default" ? "Default" : ov === "allow" ? "Allow" : "Deny"}
            </button>
          </div>
        );
      })}
    </div>
  );
}

// ── Shared badges ────────────────────────────────────────────────────────────

function SkillStatusBadge({ status }: { status: RegistrySkill["status"] }) {
  switch (status) {
    case "active":
      return (
        <Badge
          variant="outline"
          className="border-emerald-500/50 text-emerald-600 bg-emerald-500/10"
        >
          Active
        </Badge>
      );
    case "pending-review":
      return (
        <Badge
          variant="outline"
          className="border-amber-500/50 text-amber-600 bg-amber-500/10"
        >
          Pending Review
        </Badge>
      );
    case "disabled":
      return (
        <Badge
          variant="outline"
          className="border-muted-foreground/50 text-muted-foreground bg-muted/30"
        >
          Disabled
        </Badge>
      );
  }
}

function OriginBadge({
  provenance,
}: {
  provenance: SkillDigest["provenance"];
}) {
  const labels: Record<string, string> = {
    bundled: "Bundled",
    managed: "Managed",
    user: "User",
    project: "Project",
    plugin: "Plugin",
    mcp: "MCP",
  };
  const label = labels[provenance.originChannel] ?? provenance.originChannel;

  const trustColors: Record<string, string> = {
    high: "border-emerald-500/50 text-emerald-600 bg-emerald-500/10",
    medium: "border-amber-500/50 text-amber-600 bg-amber-500/10",
    low: "border-muted-foreground/30 text-muted-foreground bg-muted/30",
    untrusted: "border-destructive/50 text-destructive bg-destructive/10",
  };

  return (
    <Badge
      variant="outline"
      className={cn("text-[10px]", trustColors[provenance.trustTier])}
    >
      {label}
    </Badge>
  );
}

function LatencyBadge({ ms }: { ms: number }) {
  const variant = ms < 40 ? "default" : ms < 60 ? "secondary" : "destructive";
  return <Badge variant={variant}>{ms}ms</Badge>;
}
