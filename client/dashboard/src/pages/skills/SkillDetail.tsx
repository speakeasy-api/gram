import type {
  HookTraceSummary,
  LogFilter,
  Skill,
  SkillVersion,
} from "@gram/client/models/components";
import { Outlet, useOutletContext, useParams } from "react-router";
import { PageTabsTrigger, Tabs, TabsList } from "@/components/ui/tabs";
import {
  invalidateAllListSkills,
  invalidateAllSkillsListPending,
  invalidateAllSkillsListVersions,
  useGramContext,
  useSkill,
  useSkillsArchiveMutation,
  useSkillsListVersions,
} from "@gram/client/react-query";
import { useQuery, useQueryClient } from "@tanstack/react-query";

import { Button } from "@/components/ui/button";
import { CopyButton } from "@/components/ui/copy-button";
import { DotRow } from "@/components/ui/dot-row";
import { DotTable } from "@/components/ui/dot-table";
import { DownloadIcon } from "lucide-react";
import { Icon } from "@speakeasy-api/moonshine";
import { Link } from "react-router";
import { RequireScope } from "@/components/require-scope";
import { SkillVersionDiffPanel } from "@/pages/skills/components/SkillVersionDiffPanel";
import { SkillsPlaceholder } from "./SkillsPlaceholder";
import { Type } from "@/components/ui/type";
import { formatBytes } from "@/lib/format-bytes";
import { telemetryGetHooksSummary } from "@gram/client/funcs/telemetryGetHooksSummary";
import { telemetryListHooksTraces } from "@gram/client/funcs/telemetryListHooksTraces";
import { toast } from "sonner";
import { unwrapAsync } from "@gram/client/types/fp";
import { useProject } from "@/contexts/Auth";
import { useRoutes } from "@/routes";
import { useSlugs } from "@/contexts/Sdk";
import { useState } from "react";

type SkillDetailContext = {
  skill: Skill;
  activeVersion: SkillVersion | null;
  versions: SkillVersion[];
  versionCount: number;
  projectId: string;
};

export function SkillDetailRoot() {
  const queryClient = useQueryClient();
  const routes = useRoutes();
  const project = useProject();
  const { skillSlug } = useParams();
  const {
    data: skill,
    isPending: isSkillPending,
    error: skillError,
  } = useSkill({ slug: skillSlug ?? "" }, undefined, {
    enabled: Boolean(skillSlug),
  });
  const {
    data: versionsData,
    isPending: areVersionsPending,
    error: versionsError,
  } = useSkillsListVersions({ skillId: skill?.id ?? "" }, undefined, {
    enabled: Boolean(skill?.id),
  });

  const activeVersion =
    versionsData?.versions.find((version) => version.state === "active") ??
    null;
  const versions = versionsData?.versions ?? [];
  const versionCount = versions.length;

  const archiveMutation = useSkillsArchiveMutation({
    onSuccess: async () => {
      await Promise.all([
        invalidateAllListSkills(queryClient),
        invalidateAllSkillsListPending(queryClient),
        invalidateAllSkillsListVersions(queryClient),
      ]);
      toast.success("Skill archived");
      routes.skills.registry.goTo();
    },
    onError: () => {
      toast.error("Failed to archive skill");
    },
  });

  const routeParam = skill?.slug ?? skillSlug ?? "";
  const activeTab = routes.skills.registry.skill.versions.active
    ? "versions"
    : routes.skills.registry.skill.activity.active
      ? "activity"
      : routes.skills.registry.skill.install.active
        ? "install"
        : "definition";

  if (isSkillPending || (skill != null && areVersionsPending)) {
    return (
      <SkillDetailState
        title="Loading skill"
        description="Fetching skill details."
      />
    );
  }

  if (skillError || versionsError) {
    return (
      <SkillDetailState
        title="Couldn't load skill"
        description="There was a problem loading this skill. Please try again."
      />
    );
  }

  if (!skill) {
    return (
      <SkillDetailState
        title="Skill not found"
        description="The selected skill could not be found in this project."
      />
    );
  }

  const handleArchive = () => {
    archiveMutation.mutate({
      request: {
        archiveRequestBody: {
          skillId: skill.id,
        },
      },
    });
  };

  return (
    <div className="p-8">
      <div className="mx-auto max-w-6xl space-y-6">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-3">
            <Icon name="sparkles" className="text-primary h-5 w-5" />
            <Type variant="subheading" className="text-lg">
              {skill.name}
            </Type>
          </div>
          <div className="flex items-center gap-2">
            <Button
              size="sm"
              variant="ghost"
              disabled
              title="Download not implemented yet"
            >
              <DownloadIcon className="h-4 w-4" />
            </Button>
            <RequireScope scope="project:write" level="component">
              <Button
                size="sm"
                variant="destructiveGhost"
                disabled={archiveMutation.isPending}
                onClick={handleArchive}
              >
                Unpublish
              </Button>
            </RequireScope>
          </div>
        </div>
        <Type small muted className="block">
          {skill.description ||
            "No description has been set for this skill yet."}
        </Type>
        <div className="text-muted-foreground flex items-center gap-4 text-xs">
          {activeVersion?.authorName && (
            <span>by {activeVersion.authorName}</span>
          )}
          <span>Updated {formatDate(skill.updatedAt)}</span>
          <span>
            {versionCount} version{versionCount === 1 ? "" : "s"}
          </span>
          <span className="font-mono">{skill.slug}</span>
        </div>

        <Tabs value={activeTab} className="space-y-5">
          <div className="border-b">
            <TabsList className="h-auto items-stretch gap-6 rounded-none bg-transparent p-0">
              <PageTabsTrigger value="definition" asChild>
                <Link to={routes.skills.registry.skill.href(routeParam)}>
                  Definition
                </Link>
              </PageTabsTrigger>
              <PageTabsTrigger value="versions" asChild>
                <Link
                  to={routes.skills.registry.skill.versions.href(routeParam)}
                >
                  Versions
                </Link>
              </PageTabsTrigger>
              <PageTabsTrigger value="activity" asChild>
                <Link
                  to={routes.skills.registry.skill.activity.href(routeParam)}
                >
                  Activity
                </Link>
              </PageTabsTrigger>
              <PageTabsTrigger value="install" asChild>
                <Link
                  to={routes.skills.registry.skill.install.href(routeParam)}
                >
                  Install
                </Link>
              </PageTabsTrigger>
            </TabsList>
          </div>

          <Outlet
            context={{
              skill,
              activeVersion,
              versions,
              versionCount,
              projectId: project.id,
            }}
          />
        </Tabs>
      </div>
    </div>
  );
}

export function SkillDefinitionPage() {
  const { skill, activeVersion } = useSkillDetail();

  return (
    <section className="grid gap-6 xl:grid-cols-[minmax(0,2fr)_minmax(0,320px)]">
      <div className="border-border bg-card rounded-xl border p-5">
        <Type variant="subheading">Overview</Type>
        <div className="mt-4 space-y-4">
          <DefinitionBlock
            label="Description"
            value={skill.description || "No description has been set yet."}
          />
          <DefinitionBlock label="Slug" value={skill.slug} mono />
          <DefinitionBlock
            label="Skill UUID"
            value={skill.skillUuid || "Not set"}
            mono
          />
        </div>
      </div>

      <div className="border-border bg-card rounded-xl border p-5">
        <div className="flex items-center justify-between">
          <Type variant="subheading">Active Version</Type>
          {activeVersion && (
            <Type small muted className="font-mono">
              Active
            </Type>
          )}
        </div>
        <div className="mt-4 space-y-3">
          <DefinitionBlock
            label="Author"
            value={activeVersion?.authorName || "Unknown"}
          />
          <DefinitionBlock
            label="Format"
            value={activeVersion?.assetFormat || "N/A"}
          />
          <DefinitionBlock
            label="Size"
            value={activeVersion ? formatBytes(activeVersion.sizeBytes) : "N/A"}
          />
          <DefinitionBlock
            label="First seen"
            value={
              activeVersion?.firstSeenAt
                ? formatDate(activeVersion.firstSeenAt)
                : "N/A"
            }
          />
          <Button
            variant="outline"
            size="sm"
            className="w-full"
            disabled
            title="Download not implemented yet"
          >
            <DownloadIcon className="mr-1.5 h-3.5 w-3.5" />
            Download asset
          </Button>
        </div>
      </div>
    </section>
  );
}

export function SkillVersionsPage() {
  const { versions, activeVersion, projectId } = useSkillDetail();
  const [selectedVersionId, setSelectedVersionId] = useState<string | null>(
    null,
  );

  if (versions.length === 0) {
    return (
      <div className="rounded-xl border border-dashed px-8 py-16 text-center">
        <Type variant="subheading" className="mb-1">
          No versions yet
        </Type>
        <Type small muted>
          Versions appear here as skills are captured or uploaded.
        </Type>
      </div>
    );
  }

  const selectedVersion =
    versions.find((version) => version.id === selectedVersionId) ?? versions[0];

  return (
    <section className="space-y-4">
      <div className="grid gap-4 xl:grid-cols-[minmax(0,1.2fr)_minmax(0,1fr)]">
        <DotTable
          headers={[
            { label: "Version" },
            { label: "State" },
            { label: "Author" },
            { label: "Captured" },
            { label: "Size" },
            { label: "Review" },
          ]}
        >
          {versions.map((version) => (
            <DotRow
              key={version.id}
              onClick={() => setSelectedVersionId(version.id)}
              className={
                selectedVersion.id === version.id ? "bg-muted/30" : undefined
              }
            >
              <td className="px-3 py-3">
                <Type small className="font-mono">
                  {version.id.slice(0, 8)}
                </Type>
                <Type small muted className="block font-mono">
                  {version.contentSha256.slice(0, 12)}…
                </Type>
              </td>
              <td className="px-3 py-3">
                <Type small className="capitalize">
                  {version.state.replace("_", " ")}
                </Type>
              </td>
              <td className="px-3 py-3">
                <Type small muted>
                  {version.authorName || "Unknown"}
                </Type>
              </td>
              <td className="px-3 py-3">
                <Type small muted>
                  {formatDate(version.createdAt)}
                </Type>
              </td>
              <td className="px-3 py-3">
                <Type small muted>
                  {formatBytes(version.sizeBytes)}
                </Type>
              </td>
              <td className="px-3 py-3">
                {version.state === "rejected" ? (
                  <div className="space-y-1">
                    <Type small muted>
                      Rejected by {version.rejectedByUserId || "Unknown"}
                    </Type>
                    <Type
                      small
                      muted
                      className="line-clamp-2"
                      title={version.rejectedReason || ""}
                    >
                      {version.rejectedReason || "No reason provided"}
                    </Type>
                  </div>
                ) : (
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={(event) => {
                      event.stopPropagation();
                      setSelectedVersionId(version.id);
                    }}
                    disabled={version.id === activeVersion?.id}
                  >
                    Compare to active
                  </Button>
                )}
              </td>
            </DotRow>
          ))}
        </DotTable>

        <SkillVersionDiffPanel
          projectId={projectId}
          target={{
            versionId: selectedVersion.id,
            assetId: selectedVersion.assetId,
            label: `Version ${selectedVersion.id.slice(0, 8)}`,
          }}
          baseline={
            activeVersion && activeVersion.id !== selectedVersion.id
              ? {
                  versionId: activeVersion.id,
                  assetId: activeVersion.assetId,
                  label: "Active version",
                }
              : null
          }
        />
      </div>
    </section>
  );
}

export function SkillActivityPage() {
  const client = useGramContext();
  const { projectSlug } = useSlugs();
  const { skill, activeVersion } = useSkillDetail();

  const filters: LogFilter[] = [
    { path: "gram.skill.id", operator: "eq", values: [skill.id] },
    ...(activeVersion
      ? [
          {
            path: "gram.skill.version_id",
            operator: "eq" as const,
            values: [activeVersion.id],
          },
        ]
      : []),
  ];

  const { data: summaryData, isPending: summaryPending } = useQuery({
    queryKey: [
      "skills-activity-summary",
      skill.id,
      activeVersion?.id,
      projectSlug,
    ],
    queryFn: () =>
      unwrapAsync(
        telemetryGetHooksSummary(client, {
          gramProject: projectSlug,
          getHooksSummaryPayload: {
            from: new Date(Date.now() - 1000 * 60 * 60 * 24 * 14),
            to: new Date(),
            filters,
            typesToInclude: ["skill"],
          },
        }),
      ),
  });

  const { data: tracesData, isPending: tracesPending } = useQuery({
    queryKey: [
      "skills-activity-traces",
      skill.id,
      activeVersion?.id,
      projectSlug,
    ],
    queryFn: () =>
      unwrapAsync(
        telemetryListHooksTraces(client, {
          gramProject: projectSlug,
          listHooksTracesPayload: {
            from: new Date(Date.now() - 1000 * 60 * 60 * 24 * 14),
            to: new Date(),
            filters,
            typesToInclude: ["skill"],
            limit: 50,
            sort: "desc",
          },
        }),
      ),
  });

  const traces = tracesData?.traces ?? [];
  const tracesWithVersion = traces.filter((trace) =>
    Boolean(trace.skillVersionId),
  );

  const versionUseCount = tracesWithVersion.reduce<Record<string, number>>(
    (acc, trace) => {
      const versionId = trace.skillVersionId;
      if (!versionId) {
        return acc;
      }
      acc[versionId] = (acc[versionId] ?? 0) + 1;
      return acc;
    },
    {},
  );

  const totalEvents = summaryData?.totalEvents ?? traces.length;
  const totalSessions = summaryData?.totalSessions ?? traces.length;
  const uniqueUsers = summaryData?.users.length ?? 0;

  return (
    <section className="space-y-6">
      <div className="grid gap-4 md:grid-cols-3">
        <DefinitionBlock
          label="Skill events (14d)"
          value={summaryPending ? "…" : String(totalEvents)}
        />
        <DefinitionBlock
          label="Sessions (14d)"
          value={summaryPending ? "…" : String(totalSessions)}
        />
        <DefinitionBlock
          label="Unique users (14d)"
          value={summaryPending ? "…" : String(uniqueUsers)}
        />
      </div>

      <div className="grid gap-6 xl:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
        <div className="border-border bg-card rounded-xl border p-5">
          <Type variant="subheading">Per-version usage</Type>
          {summaryPending && tracesPending ? (
            <Type small muted className="mt-3">
              Loading usage…
            </Type>
          ) : Object.keys(versionUseCount).length === 0 ? (
            <Type small muted className="mt-3">
              No version-linked usage yet.
            </Type>
          ) : (
            <div className="mt-3 space-y-2">
              {Object.entries(versionUseCount)
                .sort((a, b) => b[1] - a[1])
                .map(([versionId, count]) => (
                  <div
                    key={versionId}
                    className="border-border bg-muted/20 flex items-center justify-between rounded-lg border px-3 py-2"
                  >
                    <Type small className="font-mono">
                      {versionId.slice(0, 8)}
                    </Type>
                    <Type small muted>
                      {count} trace{count === 1 ? "" : "s"}
                    </Type>
                  </div>
                ))}
            </div>
          )}
        </div>

        <div className="border-border bg-card rounded-xl border p-5">
          <Type variant="subheading">Recent traces</Type>
          {tracesPending ? (
            <Type small muted className="mt-3">
              Loading traces…
            </Type>
          ) : traces.length === 0 ? (
            <Type small muted className="mt-3">
              No traces found for this skill in the last 14 days.
            </Type>
          ) : (
            <div className="mt-3 space-y-2">
              {traces.slice(0, 12).map((trace) => (
                <SkillTraceRow key={trace.traceId} trace={trace} />
              ))}
            </div>
          )}
        </div>
      </div>
    </section>
  );
}

export function SkillInstallPage() {
  const { skill, activeVersion } = useSkillDetail();

  const installSnippet = [
    "# Registry reference (POC)",
    `skill_slug=${skill.slug}`,
    activeVersion
      ? `skill_version_id=${activeVersion.id}`
      : "# no active version",
    activeVersion
      ? `content_sha256=${activeVersion.contentSha256}`
      : "# no content hash",
    "",
    "# Use these values when wiring manual installs/pinning in local workflows.",
  ].join("\n");

  return (
    <section className="space-y-6">
      <div className="grid gap-4 md:grid-cols-2">
        <DefinitionWithCopy label="Skill slug" value={skill.slug} mono />
        <DefinitionWithCopy
          label="Skill UUID"
          value={skill.skillUuid || "Not set"}
          mono
        />
        <DefinitionWithCopy
          label="Active version ID"
          value={activeVersion?.id || "No active version"}
          mono
        />
        <DefinitionWithCopy
          label="Active content SHA-256"
          value={activeVersion?.contentSha256 || "No active version"}
          mono
        />
      </div>

      <div className="border-border bg-card rounded-xl border p-5">
        <div className="mb-2 flex items-center justify-between">
          <Type variant="subheading">Install snippet</Type>
          <CopyButton text={installSnippet} tooltip="Copy snippet" />
        </div>
        <pre className="bg-muted/30 border-border overflow-x-auto rounded-lg border p-4 text-xs leading-5">
          <code>{installSnippet}</code>
        </pre>
      </div>

      <div className="border-border bg-card rounded-xl border p-5">
        <Type variant="subheading">Download</Type>
        <Type small muted className="mt-2 block">
          Direct asset download wiring is pending the skill asset endpoint. This
          tab already exposes the exact identifiers needed for pinned installs.
        </Type>
      </div>
    </section>
  );
}

function useSkillDetail() {
  return useOutletContext<SkillDetailContext>();
}

function DefinitionBlock({
  label,
  value,
  mono = false,
}: {
  label: string;
  value: string;
  mono?: boolean;
}) {
  return (
    <div className="border-border bg-muted/20 rounded-lg border px-4 py-3">
      <Type small muted className="block text-[11px]">
        {label}
      </Type>
      <Type
        small
        className={mono ? "mt-1 block truncate font-mono" : "mt-1 block"}
        title={mono ? value : undefined}
      >
        {value}
      </Type>
    </div>
  );
}

function DefinitionWithCopy({
  label,
  value,
  mono = false,
}: {
  label: string;
  value: string;
  mono?: boolean;
}) {
  return (
    <div className="border-border bg-muted/20 rounded-lg border px-4 py-3">
      <div className="flex items-center justify-between gap-2">
        <Type small muted className="block text-[11px]">
          {label}
        </Type>
        <CopyButton text={value} size="icon-sm" tooltip={`Copy ${label}`} />
      </div>
      <Type
        small
        className={mono ? "mt-1 block truncate font-mono" : "mt-1 block"}
        title={mono ? value : undefined}
      >
        {value}
      </Type>
    </div>
  );
}

function SkillTraceRow({ trace }: { trace: HookTraceSummary }) {
  const startedAt = new Date(
    Number(BigInt(trace.startTimeUnixNano) / 1_000_000n),
  );

  return (
    <div className="border-border bg-muted/20 rounded-lg border px-3 py-2">
      <div className="flex items-center justify-between gap-2">
        <Type small className="font-mono">
          {trace.traceId.slice(0, 10)}
        </Type>
        <Type small muted>
          {formatDate(startedAt)}
        </Type>
      </div>
      <div className="mt-1 flex flex-wrap items-center gap-2">
        <Type small muted>
          {trace.skillName || "Unknown skill"}
        </Type>
        {trace.skillVersionId ? (
          <Type small muted className="font-mono">
            v:{trace.skillVersionId.slice(0, 8)}
          </Type>
        ) : null}
        {trace.userEmail ? (
          <Type small muted className="truncate">
            {trace.userEmail}
          </Type>
        ) : null}
      </div>
    </div>
  );
}

function SkillDetailState({
  title,
  description,
}: {
  title: string;
  description: string;
}) {
  return (
    <div className="p-8">
      <div className="mx-auto max-w-6xl">
        <SkillsPlaceholder title={title} description={description} />
      </div>
    </div>
  );
}

function formatDate(date: Date) {
  return new Intl.DateTimeFormat("en-GB", {
    month: "short",
    day: "numeric",
    year: "numeric",
  }).format(date);
}
