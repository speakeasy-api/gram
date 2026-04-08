import { Page } from "@/components/page-layout";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import { DotCard } from "@/components/ui/dot-card";
import { DotRow } from "@/components/ui/dot-row";
import { DotTable } from "@/components/ui/dot-table";
import { Switch } from "@/components/ui/switch";
import {
  PageTabsTrigger,
  Tabs,
  TabsContent,
  TabsList,
} from "@/components/ui/tabs";
import { Type } from "@/components/ui/type";
import { useViewMode, ViewToggle } from "@/components/ui/view-toggle";
import { cn } from "@/lib/utils";
import { useRoutes } from "@/routes";
import { Icon } from "@speakeasy-api/moonshine";
import { SparklesIcon } from "lucide-react";
import { useMemo, useState } from "react";
import { Outlet, useParams } from "react-router";
import {
  type CaptureSettings,
  type RegistrySkill,
  formatDate,
  MOCK_ALL_ROLES,
  MOCK_CAPTURE_SETTINGS,
  MOCK_REGISTRY_SKILLS,
} from "../context/mock-data";

export function SkillsRoot() {
  const routes = useRoutes();
  const { skillId } = useParams<{ skillId: string }>();
  const isOnDetail = Boolean(skillId);
  const [activeTab, setActiveTab] = useState("registry");

  // Force registry tab when viewing a skill detail
  const effectiveTab = isOnDetail ? "registry" : activeTab;

  const handleTabChange = (value: string) => {
    if (isOnDetail) {
      // Navigate back to skills index before switching tabs
      routes.skills.goTo();
    }
    setActiveTab(value);
  };

  const pendingCount = MOCK_REGISTRY_SKILLS.filter(
    (s) => s.status === "pending-review",
  ).length;

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs fullWidth />
      </Page.Header>
      <Page.Body fullWidth noPadding>
        <Tabs
          value={effectiveTab}
          onValueChange={handleTabChange}
          className="flex flex-col h-full"
        >
          <div className="border-b">
            <div className="px-8">
              <TabsList className="h-auto bg-transparent p-0 gap-6 rounded-none items-stretch">
                <PageTabsTrigger value="registry">Registry</PageTabsTrigger>
                <PageTabsTrigger value="review">
                  Review
                  {pendingCount > 0 && (
                    <span className="ml-1.5 inline-flex items-center justify-center h-5 min-w-5 px-1 rounded-full bg-amber-500 text-white text-xs font-medium leading-none">
                      {pendingCount}
                    </span>
                  )}
                </PageTabsTrigger>
                <PageTabsTrigger value="insights">Insights</PageTabsTrigger>
                <PageTabsTrigger value="settings">Settings</PageTabsTrigger>
              </TabsList>
            </div>
          </div>
          <TabsContent
            value="registry"
            className="flex-1 min-h-0 overflow-y-auto"
          >
            <Outlet />
          </TabsContent>
          <TabsContent
            value="review"
            className="flex-1 min-h-0 p-8 overflow-y-auto"
          >
            <ReviewTab />
          </TabsContent>
          <TabsContent
            value="insights"
            className="flex-1 min-h-0 p-8 overflow-y-auto"
          >
            <InsightsTab />
          </TabsContent>
          <TabsContent
            value="settings"
            className="flex-1 min-h-0 p-8 overflow-y-auto"
          >
            <SettingsTab />
          </TabsContent>
        </Tabs>
      </Page.Body>
    </Page>
  );
}

// ── Filter type ─────────────────────────────────────────────────────────────

/** "all" or a specific role name from MOCK_ALL_ROLES. */
type SkillFilter = string;

// ── Skills Page (index — renders inside Registry tab) ───────────────────────

export default function SkillsPage() {
  return (
    <div className="p-8">
      <RegistryTab />
    </div>
  );
}

// ── Registry Tab ────────────────────────────────────────────────────────────

function RegistryTab() {
  const routes = useRoutes();
  const [viewMode, setViewMode] = useViewMode();
  const [filter, setFilter] = useState<SkillFilter>("all");
  const [uploadDialogOpen, setUploadDialogOpen] = useState(false);
  const skills = MOCK_REGISTRY_SKILLS;

  const isVisibleToRole = (skill: RegistrySkill, role: string) => {
    const v = skill.visibility;
    if (!v || v.mode === "all") return true;
    if (v.mode === "none") return false;
    if (v.mode === "allow") return v.roles.includes(role);
    if (v.mode === "deny") return !v.roles.includes(role);
    return true;
  };

  const filtered = useMemo(() => {
    if (filter === "all") return skills;
    return skills.filter((s) => isVisibleToRole(s, filter));
  }, [skills, filter]);

  const filterCounts = useMemo(() => {
    const counts: Record<string, number> = { all: skills.length };
    for (const role of MOCK_ALL_ROLES) {
      counts[role] = skills.filter((s) => isVisibleToRole(s, role)).length;
    }
    return counts;
  }, [skills]);

  const filters: { value: SkillFilter; label: string }[] = [
    { value: "all", label: "All" },
    ...MOCK_ALL_ROLES.map((role) => ({ value: role, label: role })),
  ];

  const navigateToSkill = (skillId: string) => {
    routes.skills.detail.goTo(skillId);
  };

  return (
    <div className="space-y-6">
      {/* Filter bar + view toggle + upload */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-1 rounded-lg border border-border bg-card p-1">
          {filters.map(({ value, label }) => (
            <button
              key={value}
              onClick={() => setFilter(value)}
              className={cn(
                "px-3 py-1.5 text-xs font-medium rounded-md transition-colors flex items-center gap-1.5",
                filter === value
                  ? "bg-foreground text-background"
                  : "text-muted-foreground hover:text-foreground hover:bg-muted/50",
              )}
            >
              {label}
              <span
                className={cn(
                  "inline-flex items-center justify-center h-4 min-w-4 px-1 rounded-full text-[10px] font-medium leading-none",
                  filter === value
                    ? "bg-background/20 text-background"
                    : "bg-muted text-muted-foreground",
                  value === "pending-review" &&
                    filterCounts[value] > 0 &&
                    filter !== value &&
                    "bg-amber-500/20 text-amber-600",
                )}
              >
                {filterCounts[value]}
              </span>
            </button>
          ))}
        </div>
        <div className="flex items-center gap-3">
          <ViewToggle value={viewMode} onChange={setViewMode} />
          <Button size="sm" onClick={() => setUploadDialogOpen(true)}>
            <Icon name="upload" className="h-3.5 w-3.5 mr-1.5" />
            Upload Skill
          </Button>
        </div>
      </div>

      {/* Skills list */}
      {filtered.length === 0 ? (
        <div className="flex items-center justify-center rounded-lg border border-dashed border-border bg-card h-[200px]">
          <div className="text-center space-y-2">
            <Icon
              name="sparkles"
              className="h-10 w-10 text-muted-foreground/50 mx-auto"
            />
            <Type variant="subheading" className="text-muted-foreground">
              No skills match this filter
            </Type>
            <Type small muted>
              Try a different filter or upload a new skill.
            </Type>
          </div>
        </div>
      ) : viewMode === "table" ? (
        <SkillsTable skills={filtered} onNavigate={navigateToSkill} />
      ) : (
        <div className="grid grid-cols-1 xl:grid-cols-2 gap-6">
          {filtered.map((skill) => (
            <SkillCard
              key={skill.id}
              skill={skill}
              onClick={() => navigateToSkill(skill.id)}
            />
          ))}
        </div>
      )}

      {/* Upload dialog */}
      <UploadSkillDialog
        open={uploadDialogOpen}
        onOpenChange={setUploadDialogOpen}
      />
    </div>
  );
}

// ── Table View ────────────────────────────────────────────────────────────

function SkillsTable({
  skills,
  onNavigate,
}: {
  skills: RegistrySkill[];
  onNavigate: (id: string) => void;
}) {
  return (
    <DotTable
      headers={[
        { label: "Name" },
        { label: "Status" },
        { label: "Installations" },
        { label: "% Latest" },
        { label: "Avg tokens" },
        { label: "Version" },
      ]}
    >
      {skills.map((skill) => (
        <DotRow
          key={skill.id}
          icon={<SparklesIcon className="w-5 h-5 text-muted-foreground" />}
          onClick={() => onNavigate(skill.id)}
        >
          <td className="px-3 py-3">
            <Type
              variant="subheading"
              as="div"
              className="truncate text-sm group-hover:text-primary transition-colors"
              title={skill.name}
            >
              {skill.name}
            </Type>
            <Type small muted className="truncate block mt-0.5">
              {skill.description}
            </Type>
          </td>
          <td className="px-3 py-3">
            <SkillStatusBadge status={skill.status} />
          </td>
          <td className="px-3 py-3">
            <Type small muted>
              {skill.insights.installations}
            </Type>
          </td>
          <td className="px-3 py-3">
            <Badge
              variant="outline"
              className={cn(
                "text-[10px]",
                skill.insights.pctOnLatest >= 90
                  ? "border-emerald-500/50 text-emerald-600 bg-emerald-500/10"
                  : skill.insights.pctOnLatest >= 70
                    ? "border-amber-500/50 text-amber-600 bg-amber-500/10"
                    : "border-destructive/50 text-destructive bg-destructive/10",
              )}
            >
              {skill.insights.pctOnLatest}%
            </Badge>
          </td>
          <td className="px-3 py-3">
            <Type small muted>
              {skill.insights.avgTokens.toLocaleString()}
            </Type>
          </td>
          <td className="px-3 py-3">
            <code className="text-xs font-mono text-muted-foreground">
              v{skill.digests.length}
            </code>
          </td>
        </DotRow>
      ))}
    </DotTable>
  );
}

// ── Card View ─────────────────────────────────────────────────────────────

function SkillCard({
  skill,
  onClick,
}: {
  skill: RegistrySkill;
  onClick: () => void;
}) {
  return (
    <DotCard
      icon={<SparklesIcon className="w-8 h-8 text-muted-foreground" />}
      onClick={onClick}
      className="cursor-pointer"
    >
      <div className="flex flex-col flex-1 min-w-0">
        <div className="flex items-center gap-2 mb-1">
          <Type
            variant="subheading"
            className="truncate group-hover:text-primary transition-colors"
          >
            {skill.name}
          </Type>
          <SkillStatusBadge status={skill.status} />
        </div>
        <Type small muted className="line-clamp-2">
          {skill.description}
        </Type>
        <div className="mt-auto pt-3 flex items-center justify-between">
          <div className="flex items-center gap-3 text-xs text-muted-foreground">
            <span>{skill.insights.installations} installs</span>
            <span>{skill.insights.pctOnLatest}% on latest</span>
            <span>{skill.insights.avgTokens} tokens</span>
          </div>
          <code className="text-xs font-mono text-muted-foreground">
            v{skill.digests.length}
          </code>
        </div>
      </div>
    </DotCard>
  );
}

// ── Badges ────────────────────────────────────────────────────────────────

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

// ── Review Tab ────────────────────────────────────────────────────────────

function ReviewTab() {
  const routes = useRoutes();
  const pending = useMemo(
    () => MOCK_REGISTRY_SKILLS.filter((s) => s.status === "pending-review"),
    [],
  );

  if (pending.length === 0) {
    return (
      <div className="flex items-center justify-center rounded-lg border border-dashed border-border bg-card h-[200px]">
        <div className="text-center space-y-2">
          <Icon
            name="check-circle"
            className="h-10 w-10 text-emerald-500/50 mx-auto"
          />
          <Type variant="subheading" className="text-muted-foreground">
            No skills pending review
          </Type>
          <Type small muted>
            All skills have been reviewed and approved.
          </Type>
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-4">
      {pending.map((skill) => {
        const latestDigest = skill.digests[0];
        const audit = latestDigest?.audit;
        return (
          <div
            key={skill.id}
            className="rounded-lg border border-amber-500/30 bg-card overflow-hidden"
          >
            <div className="flex items-center justify-between px-4 py-3 border-b border-border">
              <div className="flex items-center gap-3">
                <Icon name="sparkles" className="h-5 w-5 text-amber-500" />
                <div>
                  <Type variant="subheading">{skill.name}</Type>
                  <Type small muted className="block mt-0.5">
                    {skill.description}
                  </Type>
                </div>
              </div>
              <div className="flex items-center gap-2">
                {audit && (
                  <Badge
                    variant="outline"
                    className={cn(
                      "text-[10px] uppercase",
                      audit.riskLevel === "safe" &&
                        "border-emerald-500/50 text-emerald-600 bg-emerald-500/10",
                      audit.riskLevel === "caution" &&
                        "border-amber-500/50 text-amber-600 bg-amber-500/10",
                      (audit.riskLevel === "warning" ||
                        audit.riskLevel === "critical") &&
                        "border-destructive/50 text-destructive bg-destructive/10",
                    )}
                  >
                    {audit.riskLevel}
                  </Badge>
                )}
                <Button size="sm" variant="outline" className="text-xs">
                  Reject
                </Button>
                <Button size="sm" className="text-xs">
                  Approve
                </Button>
              </div>
            </div>

            {/* Metadata */}
            <div className="px-4 py-2.5 border-b border-border flex items-center gap-4 text-xs text-muted-foreground">
              <span>
                Pushed by{" "}
                <span className="font-medium text-foreground">
                  {latestDigest?.pushedBy}
                </span>
              </span>
              {latestDigest && <span>{formatDate(latestDigest.pushedAt)}</span>}
              {latestDigest && (
                <code className="font-mono text-[10px]">
                  {latestDigest.contentHash.slice(0, 19)}
                </code>
              )}
              {latestDigest && (
                <span>{latestDigest.bodyBytes.toLocaleString()} B</span>
              )}
            </div>

            {/* Skill body preview */}
            <div className="px-4 py-3">
              <pre className="text-xs font-mono whitespace-pre-wrap text-foreground bg-muted/30 rounded-md p-3 max-h-[150px] overflow-auto">
                {skill.body}
              </pre>
            </div>

            {/* Link to detail */}
            <div className="px-4 py-2.5 border-t border-border">
              <button
                onClick={() => routes.skills.detail.goTo(skill.id)}
                className="text-xs text-primary hover:underline"
              >
                View full detail &rarr;
              </button>
            </div>
          </div>
        );
      })}
    </div>
  );
}

// ── Settings Tab ──────────────────────────────────────────────────────────

function SettingsTab() {
  const [captureSettings, setCaptureSettings] = useState<CaptureSettings>(
    MOCK_CAPTURE_SETTINGS,
  );

  return (
    <div className="max-w-2xl space-y-6">
      <CaptureSettingsSection
        settings={captureSettings}
        onChange={setCaptureSettings}
      />
    </div>
  );
}

// ── Capture Settings ──────────────────────────────────────────────────────

function CaptureSettingsSection({
  settings,
  onChange,
}: {
  settings: CaptureSettings;
  onChange: (settings: CaptureSettings) => void;
}) {
  const toggle = (key: keyof CaptureSettings) => {
    onChange({ ...settings, [key]: !settings[key] });
  };

  return (
    <div className="rounded-lg border border-border bg-card overflow-hidden">
      <div className="px-4 py-3 border-b border-border">
        <Type variant="subheading">Capture Settings</Type>
        <Type small muted className="mt-1 block">
          Configure how skills are automatically captured from agent sessions.
        </Type>
      </div>
      <div className="divide-y divide-border">
        <CaptureToggle
          label="Enable skill capture"
          description="Automatically extract skills from agent conversations"
          checked={settings.enabled}
          onChange={() => toggle("enabled")}
        />
        <CaptureToggle
          label="Capture project-level skills"
          description="Capture skills scoped to this project"
          checked={settings.captureProjectSkills}
          onChange={() => toggle("captureProjectSkills")}
          disabled={!settings.enabled}
        />
        <CaptureToggle
          label="Capture user-level skills"
          description="Capture skills scoped to individual users"
          checked={settings.captureUserSkills}
          onChange={() => toggle("captureUserSkills")}
          disabled={!settings.enabled}
        />
        <CaptureToggle
          label="Honor x-gram-ignore frontmatter"
          description="Skip files with x-gram-ignore: true in their frontmatter"
          checked={settings.ignoreWithFrontmatter}
          onChange={() => toggle("ignoreWithFrontmatter")}
          disabled={!settings.enabled}
        />
      </div>
    </div>
  );
}

function CaptureToggle({
  label,
  description,
  checked,
  onChange,
  disabled,
}: {
  label: string;
  description: string;
  checked: boolean;
  onChange: () => void;
  disabled?: boolean;
}) {
  return (
    <div
      className={cn(
        "flex items-center justify-between px-4 py-3",
        disabled && "opacity-50",
      )}
    >
      <div>
        <Type small className="font-medium block">
          {label}
        </Type>
        <Type small muted className="block">
          {description}
        </Type>
      </div>
      <Switch
        checked={checked}
        onCheckedChange={onChange}
        disabled={disabled}
      />
    </div>
  );
}

// ── Upload Skill Dialog ───────────────────────────────────────────────────

function UploadSkillDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const [content, setContent] = useState("");

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <Dialog.Content className="max-w-lg">
        <Dialog.Header>
          <Dialog.Title>Upload Skill</Dialog.Title>
          <Dialog.Description>
            Paste or write a SKILL.md file to add it to the registry. Include
            YAML frontmatter with name and description fields.
          </Dialog.Description>
        </Dialog.Header>
        <div className="py-4">
          <textarea
            value={content}
            onChange={(e) => setContent(e.target.value)}
            placeholder={`---\nname: my-skill\ndescription: Description of the skill\n---\n\nSkill instructions here...`}
            className="w-full h-48 rounded-md border border-border bg-muted/30 px-3 py-2 text-sm font-mono resize-none focus:outline-none focus:ring-2 focus:ring-ring"
          />
        </div>
        <Dialog.Footer>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button
            onClick={() => {
              onOpenChange(false);
              setContent("");
            }}
            disabled={!content.trim()}
          >
            Upload
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}

// ── Insights Tab ──────────────────────────────────────────────────────────

function InsightsTab() {
  const skills = MOCK_REGISTRY_SKILLS;

  const totals = useMemo(() => {
    const installs = skills.reduce((s, sk) => s + sk.insights.installations, 0);
    const active = skills.reduce(
      (s, sk) => s + sk.insights.activeInstallations,
      0,
    );
    const inv7d = skills.reduce((s, sk) => s + sk.insights.invocations7d, 0);
    const avgTokens =
      skills.length > 0
        ? Math.round(
            skills.reduce((s, sk) => s + sk.insights.avgTokens, 0) /
              skills.length,
          )
        : 0;
    return { installs, active, inv7d, avgTokens };
  }, [skills]);

  return (
    <div className="space-y-6">
      {/* Aggregate stats */}
      <div className="grid grid-cols-4 gap-4">
        <StatCard label="Total Installations" value={String(totals.installs)} />
        <StatCard label="Active Installations" value={String(totals.active)} />
        <StatCard
          label="Invocations (7d)"
          value={totals.inv7d.toLocaleString()}
        />
        <StatCard
          label="Avg Tokens / Skill"
          value={totals.avgTokens.toLocaleString()}
        />
      </div>

      {/* Per-skill insights table */}
      <div className="rounded-lg border border-border bg-card overflow-hidden">
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border bg-muted/30">
                <th className="px-4 py-2.5 text-left font-medium text-muted-foreground">
                  Skill
                </th>
                <th className="px-4 py-2.5 text-right font-medium text-muted-foreground">
                  Installations
                </th>
                <th className="px-4 py-2.5 text-right font-medium text-muted-foreground">
                  % Latest
                </th>
                <th className="px-4 py-2.5 text-right font-medium text-muted-foreground">
                  Avg Tokens
                </th>
                <th className="px-4 py-2.5 text-right font-medium text-muted-foreground">
                  Invocations (7d)
                </th>
                <th className="px-4 py-2.5 text-right font-medium text-muted-foreground">
                  Success
                </th>
                <th className="px-4 py-2.5 text-left font-medium text-muted-foreground">
                  Version
                </th>
              </tr>
            </thead>
            <tbody>
              {skills.map((skill) => (
                <tr
                  key={skill.id}
                  className="border-b border-border last:border-b-0 hover:bg-muted/50 transition-colors"
                >
                  <td className="px-4 py-2.5 font-medium">{skill.name}</td>
                  <td className="px-4 py-2.5 text-right tabular-nums">
                    <span className="text-foreground">
                      {skill.insights.activeInstallations}
                    </span>
                    <span className="text-muted-foreground">
                      {" / "}
                      {skill.insights.installations}
                    </span>
                  </td>
                  <td className="px-4 py-2.5 text-right">
                    <Badge
                      variant="outline"
                      className={cn(
                        "text-[10px]",
                        skill.insights.pctOnLatest >= 90
                          ? "border-emerald-500/50 text-emerald-600 bg-emerald-500/10"
                          : skill.insights.pctOnLatest >= 70
                            ? "border-amber-500/50 text-amber-600 bg-amber-500/10"
                            : "border-destructive/50 text-destructive bg-destructive/10",
                      )}
                    >
                      {skill.insights.pctOnLatest}%
                    </Badge>
                  </td>
                  <td className="px-4 py-2.5 text-right tabular-nums text-muted-foreground">
                    {skill.insights.avgTokens.toLocaleString()}
                  </td>
                  <td className="px-4 py-2.5 text-right tabular-nums">
                    {skill.insights.invocations7d.toLocaleString()}
                  </td>
                  <td className="px-4 py-2.5 text-right tabular-nums">
                    <span
                      className={
                        skill.insights.successRate >= 99
                          ? "text-emerald-600"
                          : skill.insights.successRate >= 95
                            ? "text-foreground"
                            : "text-destructive"
                      }
                    >
                      {skill.insights.successRate}%
                    </span>
                  </td>
                  <td className="px-4 py-2.5">
                    <code className="text-xs font-mono text-muted-foreground">
                      v{skill.digests.length}
                    </code>
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

// ── Shared helpers ────────────────────────────────────────────────────────

function StatCard({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-lg border border-border bg-card px-4 py-3">
      <Type small muted className="block">
        {label}
      </Type>
      <Type variant="subheading" className="mt-1 block">
        {value}
      </Type>
    </div>
  );
}
