import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import {
  RouteNotFoundState,
  SecondaryRouteAction,
} from "@/components/route-not-found-state";
import { ErrorAlert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Checkbox } from "@/components/ui/Checkbox";
import { Skeleton, SkeletonTable } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { useProject } from "@/contexts/Auth";
import { useDrainInfiniteQuery } from "@/hooks/useDrainInfiniteQuery";
import { Markdown } from "@/elements/components/Markdown";
import { dateTimeFormatters, HumanizeDateTime } from "@/lib/dates";
import { isNotFoundError } from "@/lib/route-errors";
import {
  DangerSettingsSection,
  SettingsSection,
} from "@/components/detail/settings-section";
import { useRoutes } from "@/routes";
import type { SkillVersion } from "@gram/client/models/components/skillversion.js";
import { useSkill } from "@gram/client/react-query/skill.js";
import { useSkillVersionsInfinite } from "@gram/client/react-query/skillVersions.js";
import { Badge } from "@/components/ui/Badge";
import { type Column, Table } from "@/components/ui/Table";
import { lazy, Suspense, useEffect, useState } from "react";
import { useLocation, useNavigate, useParams } from "react-router";
import {
  ArchiveSkillDialog,
  type ArchiveSkillTarget,
} from "./ArchiveSkillDialog";
import { EditSkillDetailsDialog } from "./EditSkillDetailsDialog";
import { SkillDistributionsSection } from "./SkillDistributionsSection";
import { SkillFeedbackSection } from "./SkillFeedbackSection";
import {
  SKILL_INSIGHTS_SECTION_ID,
  SkillInsightsSection,
} from "./SkillInsightsSection";
import {
  SKILL_ADOPTION_SECTION_ID,
  SKILL_TIMELINE_SECTION_ID,
  SkillActivitySections,
} from "./SkillActivitySections";
import { stripSkillFrontmatter } from "./skill-manifest";
import { SkillManifestDialog } from "./SkillManifestDialog";
import { SkillPluginBanner } from "./SkillPluginBanner";
import { SkillValidationErrors } from "./SkillValidationErrors";
import { RestoreSkillVersionDialog } from "./RestoreSkillVersionDialog";
import { SuggestedSkillEditSection } from "./SuggestedSkillEditSection";
import {
  selectDiffVersions,
  type VersionChangeDirection,
  versionChangeDirection,
} from "./version-selection";

const SkillTextDiff = lazy(() => import("./SkillTextDiff"));

export const SKILL_MANIFEST_SECTION_ID = "manifest";
export const SKILL_FRONTMATTER_SECTION_ID = "frontmatter";
export const SKILL_DISTRIBUTIONS_SECTION_ID = "distributions";
export const SKILL_VERSIONS_SECTION_ID = "versions";
const SKILL_DANGER_SECTION_ID = "danger";

const SKILL_SECTION_IDS: readonly string[] = [
  SKILL_ADOPTION_SECTION_ID,
  SKILL_INSIGHTS_SECTION_ID,
  SKILL_MANIFEST_SECTION_ID,
  SKILL_FRONTMATTER_SECTION_ID,
  SKILL_DISTRIBUTIONS_SECTION_ID,
  SKILL_VERSIONS_SECTION_ID,
  SKILL_TIMELINE_SECTION_ID,
  SKILL_DANGER_SECTION_ID,
];

function versionAnchorLabel(
  version: SkillVersion,
  currentVersion: SkillVersion,
): string {
  const hash = version.canonicalSha256.slice(0, 8);
  if (version.id === currentVersion.id)
    return `Version ${hash}, current version`;
  if (!version.specValid) return `Version ${hash}, invalid version`;
  const direction = versionChangeDirection(version, currentVersion);
  if (direction === "backward") return `Version ${hash}, roll back target`;
  return `Version ${hash}, promotion target`;
}

function useScrollToSectionHash(): void {
  const location = useLocation();

  useEffect(() => {
    const targetId = location.hash.replace("#", "");
    if (!SKILL_SECTION_IDS.includes(targetId)) {
      return;
    }

    const animationFrame = window.requestAnimationFrame(() => {
      document
        .getElementById(targetId)
        ?.scrollIntoView({ behavior: "smooth", block: "start" });
    });

    return () => window.cancelAnimationFrame(animationFrame);
  }, [location.hash]);
}

export default function SkillDetail(): JSX.Element {
  const { skillId } = useParams<{ skillId: string }>();
  const routes = useRoutes();
  const skillQuery = useSkill({ id: skillId ?? "" }, undefined, {
    throwOnError: false,
    enabled: !!skillId,
  });

  // Only a 404 means the skill is gone; other failures (transient 5xx, stale
  // grants) surface through the route error boundary with a retry path.
  if (
    skillQuery.error &&
    !skillQuery.data &&
    !isNotFoundError(skillQuery.error)
  ) {
    throw skillQuery.error;
  }

  if (!skillId || (skillQuery.error && !skillQuery.data)) {
    return (
      <Page>
        <Page.Header>
          <Page.Header.Breadcrumbs />
        </Page.Header>
        <Page.Body>
          <RouteNotFoundState
            title="Skill not found"
            description="This skill may have been archived or removed from this project."
            action={
              <routes.skills.Link>
                <SecondaryRouteAction>Back to skills</SecondaryRouteAction>
              </routes.skills.Link>
            }
          />
        </Page.Body>
      </Page>
    );
  }

  if (skillQuery.isPending || !skillQuery.data) {
    return <SkillDetailLoading />;
  }

  const { skill } = skillQuery.data;

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs
          substitutions={{ [skillId]: skill.displayName }}
        />
      </Page.Header>
      <Page.Body fullWidth className="gap-0">
        {/* Name, badges, and metadata live in the sidebar's at-a-glance card */}
        <div className="mx-auto w-full max-w-[1270px] flex-1 space-y-10 px-8 py-8">
          <SkillDetailSections
            skillId={skillId}
            skillQueryData={skillQuery.data}
          />
        </div>
      </Page.Body>
    </Page>
  );
}

function SkillDetailSections({
  skillId,
  skillQueryData,
}: {
  skillId: string;
  skillQueryData: NonNullable<ReturnType<typeof useSkill>["data"]>;
}): JSX.Element {
  const project = useProject();
  const routes = useRoutes();
  const navigate = useNavigate();
  const [editOpen, setEditOpen] = useState(false);
  const [detailsOpen, setDetailsOpen] = useState(false);
  const [archiveTarget, setArchiveTarget] = useState<ArchiveSkillTarget | null>(
    null,
  );
  useScrollToSectionHash();

  const { skill, latestVersion } = skillQueryData;
  const versionsQuery = useSkillVersionsInfinite({ id: skill.id }, undefined, {
    throwOnError: false,
  });
  useDrainInfiniteQuery(versionsQuery);
  const versionsLoading =
    !versionsQuery.error &&
    (versionsQuery.isPending ||
      versionsQuery.hasNextPage ||
      versionsQuery.isFetchingNextPage);
  const versions =
    versionsQuery.data?.pages.flatMap((page) => page.result.versions) ?? [];
  const versionLabels = new Map(
    [...versions]
      .sort(
        (left, right) => left.createdAt.getTime() - right.createdAt.getTime(),
      )
      .map((version, index) => [
        version.id,
        `v${skill.versionCount - versions.length + index + 1} (${version.canonicalSha256.slice(0, 8)})`,
      ]),
  );
  const body = latestVersion
    ? stripSkillFrontmatter(latestVersion.content)
    : "";
  const frontmatterEntries = Object.entries(
    latestVersion?.frontmatter ?? {},
  ).filter(([key]) => key !== "name" && key !== "description");

  return (
    <>
      <SkillPluginBanner skill={skill} />

      <SettingsSection>
        <SettingsSection.Header>
          <SettingsSection.Title>Skill details</SettingsSection.Title>
          <SettingsSection.Description>
            Registry identity and presentation metadata.
          </SettingsSection.Description>
        </SettingsSection.Header>
        <SettingsSection.Panel>
          <SettingsSection.Body>
            <dl className="grid gap-4 sm:grid-cols-3">
              <div>
                <dt className="text-muted-foreground text-xs">
                  Canonical name
                </dt>
                <dd className="mt-1 font-mono text-sm">{skill.name}</dd>
              </div>
              <div>
                <dt className="text-muted-foreground text-xs">Display name</dt>
                <dd className="mt-1 text-sm">{skill.displayName}</dd>
              </div>
              <div>
                <dt className="text-muted-foreground text-xs">Summary</dt>
                <dd className="mt-1 text-sm">{skill.summary || "None"}</dd>
              </div>
              <div className="sm:col-span-3">
                <dt className="text-muted-foreground text-xs">Tags</dt>
                <dd className="mt-1">
                  {skill.tags.length > 0 ? (
                    <div className="flex flex-wrap gap-1.5">
                      {skill.tags.map((tag) => (
                        <Badge key={tag} variant="neutral" className="text-xs">
                          {tag}
                        </Badge>
                      ))}
                    </div>
                  ) : (
                    <span className="text-sm">None</span>
                  )}
                </dd>
              </div>
            </dl>
          </SettingsSection.Body>
          <SettingsSection.Footer>
            <SettingsSection.FooterHint>
              Renaming keeps activation attribution on this skill.
            </SettingsSection.FooterHint>
            <SettingsSection.FooterActions>
              <RequireScope
                scope="skill:write"
                resourceId={project.id}
                level="component"
              >
                <Button size="sm" onClick={() => setDetailsOpen(true)}>
                  Edit details
                </Button>
              </RequireScope>
            </SettingsSection.FooterActions>
          </SettingsSection.Footer>
        </SettingsSection.Panel>
      </SettingsSection>

      <SkillActivitySections
        data={skillQueryData}
        versionLabels={versionLabels}
        versionsLoading={versionsLoading}
      />

      {latestVersion && (
        <SuggestedSkillEditSection
          skillId={skillId}
          latestVersion={latestVersion}
        />
      )}

      <SkillInsightsSection
        data={skillQueryData}
        versionLabels={versionLabels}
        versionsLoading={versionsLoading}
        versionsError={versionsQuery.error}
      />

      <SettingsSection id={SKILL_MANIFEST_SECTION_ID}>
        <SettingsSection.Header>
          <SettingsSection.Title>SKILL.md</SettingsSection.Title>
          <SettingsSection.Description>
            The current version of this skill's manifest, exactly as agents load
            it.
          </SettingsSection.Description>
        </SettingsSection.Header>
        <SettingsSection.Panel>
          <SettingsSection.Body>
            {latestVersion && !latestVersion.specValid && (
              <ValidationErrors errors={latestVersion.validationErrors} />
            )}
            <div className="overflow-x-auto">
              {latestVersion ? (
                <ManifestBody body={body} />
              ) : (
                <Text small muted>
                  Manifest content has not been captured for this observed
                  skill.
                </Text>
              )}
            </div>
          </SettingsSection.Body>
          {latestVersion && (
            <SettingsSection.Footer>
              <SettingsSection.FooterHint>
                Current version{" "}
                <span className="font-mono">
                  {latestVersion.canonicalSha256.slice(0, 8)}
                </span>{" "}
                · updated <HumanizeDateTime date={skill.updatedAt} />
              </SettingsSection.FooterHint>
              <SettingsSection.FooterActions>
                <RequireScope
                  scope="skill:write"
                  resourceId={project.id}
                  level="component"
                >
                  <Button size="sm" onClick={() => setEditOpen(true)}>
                    Edit SKILL.md
                  </Button>
                </RequireScope>
              </SettingsSection.FooterActions>
            </SettingsSection.Footer>
          )}
        </SettingsSection.Panel>
      </SettingsSection>

      {frontmatterEntries.length > 0 && (
        <SettingsSection id={SKILL_FRONTMATTER_SECTION_ID}>
          <SettingsSection.Header>
            <SettingsSection.Title>Frontmatter</SettingsSection.Title>
            <SettingsSection.Description>
              Additional metadata declared in the manifest's frontmatter.
            </SettingsSection.Description>
          </SettingsSection.Header>
          <SettingsSection.Panel>
            <SettingsSection.Body>
              <dl className="space-y-1.5">
                {frontmatterEntries.map(([key, value]) => (
                  <div key={key} className="flex gap-3 text-sm">
                    <dt className="text-muted-foreground shrink-0 font-mono">
                      {key}
                    </dt>
                    <dd className="min-w-0 break-words font-mono">
                      {typeof value === "string"
                        ? value
                        : JSON.stringify(value)}
                    </dd>
                  </div>
                ))}
              </dl>
            </SettingsSection.Body>
          </SettingsSection.Panel>
        </SettingsSection>
      )}

      {latestVersion && (
        <SettingsSection id={SKILL_DISTRIBUTIONS_SECTION_ID}>
          <SettingsSection.Header>
            <SettingsSection.Title>Plugin distributions</SettingsSection.Title>
            <SettingsSection.Description>
              Used by {skillQueryData.assistantCount}{" "}
              {skillQueryData.assistantCount === 1 ? "assistant" : "assistants"}
              . The plugins carrying this skill ship it inside the plugin
              package for everyone who installs it.
            </SettingsSection.Description>
          </SettingsSection.Header>
          <SkillDistributionsSection skillId={skillId} />
        </SettingsSection>
      )}

      {latestVersion && (
        <SettingsSection id={SKILL_VERSIONS_SECTION_ID}>
          <SettingsSection.Header>
            <SettingsSection.Title>Version history</SettingsSection.Title>
            <SettingsSection.Description>
              Versions are ordered by creation date, newest first. After a
              rollback, the current version may appear below newer versions.
            </SettingsSection.Description>
          </SettingsSection.Header>
          <VersionHistory skillId={skillId} currentVersion={latestVersion} />
        </SettingsSection>
      )}

      <SkillFeedbackSection skillId={skillId} projectId={project.id} />

      <DangerSettingsSection id={SKILL_DANGER_SECTION_ID}>
        <DangerSettingsSection.Header>
          <DangerSettingsSection.Title>Danger zone</DangerSettingsSection.Title>
        </DangerSettingsSection.Header>
        <DangerSettingsSection.Panel>
          <DangerSettingsSection.Body className="flex flex-wrap items-center justify-between gap-2">
            <div className="space-y-1 m-0">
              <Text className="text-sm font-semibold">Archive this skill</Text>
              <Text small muted className="max-w-xl">
                Archiving removes the skill from this project's catalog and
                revokes its plugin distributions.
              </Text>
            </div>
            <RequireScope
              scope="skill:write"
              resourceId={project.id}
              level="component"
            >
              <Button
                variant="destructive-primary"
                onClick={() =>
                  setArchiveTarget({
                    id: skill.id,
                    displayName: skill.displayName,
                  })
                }
              >
                Archive
              </Button>
            </RequireScope>
          </DangerSettingsSection.Body>
        </DangerSettingsSection.Panel>
      </DangerSettingsSection>

      {latestVersion && (
        <SkillManifestDialog
          key={editOpen ? "edit" : "closed"}
          mode="edit"
          open={editOpen}
          onOpenChange={setEditOpen}
          skillId={skill.id}
          derivedFromVersionId={latestVersion.id}
          initialContent={latestVersion.content}
        />
      )}
      <EditSkillDetailsDialog
        key={detailsOpen ? "details-open" : "details-closed"}
        skill={skill}
        open={detailsOpen}
        onOpenChange={setDetailsOpen}
      />
      <ArchiveSkillDialog
        skill={archiveTarget}
        onClose={() => setArchiveTarget(null)}
        onArchived={() => void navigate(routes.skills.href())}
      />
    </>
  );
}

function VersionHistory({
  skillId,
  currentVersion,
}: {
  skillId: string;
  currentVersion: SkillVersion;
}): JSX.Element {
  const project = useProject();
  const location = useLocation();
  const versionsQuery = useSkillVersionsInfinite({ id: skillId }, undefined, {
    throwOnError: false,
  });
  const [selectedVersions, setSelectedVersions] = useState<Set<string>>(
    () => new Set(),
  );
  const [restoreTarget, setRestoreTarget] = useState<{
    version: SkillVersion;
    direction: VersionChangeDirection;
  } | null>(null);

  const versions =
    versionsQuery.data?.pages.flatMap((page) => page.result.versions) ?? [];
  const diffVersions = selectDiffVersions(
    versions,
    selectedVersions,
    currentVersion,
  );
  const comparable = versions.length > 1;
  useEffect(() => {
    const target = document.getElementById(location.hash.slice(1));
    if (!target || !location.hash.startsWith("#version-")) return;
    target.scrollIntoView({ behavior: "smooth", block: "center" });
    target.focus();
  }, [location.hash, versions.length]);
  let loadMoreLabel = "Load more versions";
  if (versionsQuery.isFetchingNextPage) loadMoreLabel = "Loading...";
  const columns = versionColumns({
    currentVersionId: currentVersion.id,
    comparable,
    selectedVersions,
    onToggle: (versionId) => {
      setSelectedVersions((current) => {
        const next = new Set(current);
        if (next.has(versionId)) {
          next.delete(versionId);
        } else if (next.size < 2) {
          next.add(versionId);
        }
        return next;
      });
    },
    restoreAction: (version) => {
      const direction = versionChangeDirection(version, currentVersion);
      return (
        <div
          id={`version-${version.id}`}
          role="group"
          tabIndex={-1}
          aria-label={versionAnchorLabel(version, currentVersion)}
          className="focus-visible:ring-ring focus-visible:ring-2 focus-visible:outline-none"
        >
          {direction && version.specValid && (
            <RequireScope
              scope="skill:write"
              resourceId={project.id}
              level="component"
              reason="You need write access to change the current skill version."
            >
              <Button
                size="sm"
                variant="secondary"
                onClick={() => setRestoreTarget({ version, direction })}
              >
                {direction === "backward" ? "Roll back" : "Promote"}
              </Button>
            </RequireScope>
          )}
        </div>
      );
    },
  });

  return (
    <>
      <div className="space-y-4">
        {comparable && (
          <Text small muted>
            Select one version to compare it with current, or select any two
            loaded versions.
          </Text>
        )}
        {versionsQuery.isPending && !versionsQuery.data && <SkeletonTable />}
        {versionsQuery.error && !versionsQuery.data && (
          <ErrorAlert
            title="Unable to load versions"
            error={versionsQuery.error}
          />
        )}
        {versionsQuery.data && (
          <div className="overflow-x-auto">
            <Table
              columns={columns}
              data={versions}
              rowKey={(version) => version.id}
              className="min-w-[560px]"
              noResultsMessage="No versions found."
            />
          </div>
        )}
        {versionsQuery.isFetchNextPageError && (
          <LoadMoreError onRetry={() => void versionsQuery.fetchNextPage()} />
        )}
        {versionsQuery.hasNextPage && !versionsQuery.isFetchNextPageError && (
          <Button
            variant="secondary"
            disabled={versionsQuery.isFetchingNextPage}
            onClick={() => void versionsQuery.fetchNextPage()}
          >
            {loadMoreLabel}
          </Button>
        )}
        <VersionDiff versions={diffVersions} />
      </div>
      <RestoreSkillVersionDialog
        skillId={skillId}
        version={restoreTarget?.version ?? null}
        direction={restoreTarget?.direction ?? null}
        onClose={() => setRestoreTarget(null)}
      />
    </>
  );
}

function ManifestBody({ body }: { body: string }): JSX.Element {
  if (body.trim().length === 0) {
    return (
      <Text small muted>
        This manifest has no Markdown body.
      </Text>
    );
  }
  return <Markdown className="text-sm">{body}</Markdown>;
}

function SkillDetailLoading(): JSX.Element {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body fullWidth className="gap-0">
        <div
          aria-label="Loading skill"
          className="mx-auto w-full max-w-[1270px] flex-1 space-y-10 px-8 py-8"
        >
          <Skeleton className="h-36 w-full" />
          <Skeleton className="h-80 w-full" />
          <Skeleton className="h-48 w-full" />
        </div>
      </Page.Body>
    </Page>
  );
}

function ValidationErrors({
  errors,
}: {
  errors: SkillVersion["validationErrors"];
}): JSX.Element {
  return (
    <div className="border-destructive/40 bg-destructive/5 border p-4">
      <Text variant="subheading" className="text-destructive mb-2">
        Current version has validation issues
      </Text>
      <SkillValidationErrors errors={errors} />
    </div>
  );
}

function versionColumns({
  currentVersionId,
  comparable,
  selectedVersions,
  onToggle,
  restoreAction,
}: {
  currentVersionId: string;
  comparable: boolean;
  selectedVersions: Set<string>;
  onToggle: (versionId: string) => void;
  restoreAction: (version: SkillVersion) => JSX.Element;
}): Column<SkillVersion>[] {
  const compareColumn: Column<SkillVersion> = {
    key: "compare",
    header: "Compare",
    width: "90px",
    render: (version) => (
      <Checkbox
        aria-label={`Compare version ${version.canonicalSha256.slice(0, 8)}`}
        checked={selectedVersions.has(version.id)}
        disabled={
          selectedVersions.size >= 2 && !selectedVersions.has(version.id)
        }
        onCheckedChange={() => onToggle(version.id)}
      />
    ),
  };
  return [
    ...(comparable ? [compareColumn] : []),
    {
      key: "hash",
      header: "Version",
      width: "280px",
      render: (version) => (
        <div className="flex flex-wrap items-center gap-2">
          <span className="font-mono text-sm">
            {version.canonicalSha256.slice(0, 8)}
          </span>
          {version.id === currentVersionId && (
            <Badge variant="information">Current</Badge>
          )}
          {version.derivedFromVersionId && (
            <Badge variant="neutral" title={version.derivedFromVersionId}>
              Derived
            </Badge>
          )}
        </div>
      ),
    },
    {
      key: "validity",
      header: "Validity",
      width: "2fr",
      render: (version) => (
        <div className="space-y-2">
          <Badge variant={version.specValid ? "success" : "destructive"}>
            {version.specValid ? "Valid" : "Invalid"}
          </Badge>
          {!version.specValid && (
            <SkillValidationErrors errors={version.validationErrors} />
          )}
        </div>
      ),
    },
    {
      key: "activations",
      header: "Activations",
      width: "110px",
      render: (version) => <Text small>{version.seenCount}</Text>,
    },
    {
      key: "firstSeen",
      header: "First activated",
      width: "150px",
      render: (version) =>
        version.firstSeenAt ? (
          <Text
            small
            muted
            title={dateTimeFormatters.full.format(version.firstSeenAt)}
          >
            <HumanizeDateTime date={version.firstSeenAt} />
          </Text>
        ) : (
          <Text small muted>
            Never
          </Text>
        ),
    },
    {
      key: "lastSeen",
      header: "Last activated",
      width: "150px",
      render: (version) =>
        version.lastSeenAt ? (
          <Text
            small
            muted
            title={dateTimeFormatters.full.format(version.lastSeenAt)}
          >
            <HumanizeDateTime date={version.lastSeenAt} />
          </Text>
        ) : (
          <Text small muted>
            Never
          </Text>
        ),
    },
    {
      key: "created",
      header: "Created",
      width: "150px",
      render: (version) => (
        <Text small title={dateTimeFormatters.full.format(version.createdAt)}>
          <HumanizeDateTime date={version.createdAt} />
        </Text>
      ),
    },
    {
      key: "actions",
      header: "",
      width: "180px",
      render: restoreAction,
    },
  ];
}

function LoadMoreError({ onRetry }: { onRetry: () => void }): JSX.Element {
  return (
    <div className="border-destructive/40 bg-destructive/5 flex flex-wrap items-center justify-between gap-3 border p-3">
      <Text small className="text-destructive">
        Unable to load more versions.
      </Text>
      <Button size="sm" variant="secondary" onClick={onRetry}>
        Retry
      </Button>
    </div>
  );
}

function VersionDiff({
  versions,
}: {
  versions: [SkillVersion, SkillVersion] | null;
}): JSX.Element | null {
  if (!versions) return null;
  const [older, newer] = versions;
  return (
    <div className="space-y-3">
      <Text small muted mono className="text-xs tracking-wider">
        Diff · {older.canonicalSha256.slice(0, 8)} →{" "}
        {newer.canonicalSha256.slice(0, 8)}
      </Text>
      <Suspense fallback={<Skeleton className="h-80 w-full" />}>
        <SkillTextDiff
          oldContent={older.content}
          newContent={newer.content}
          oldLabel={older.canonicalSha256.slice(0, 8)}
          newLabel={newer.canonicalSha256.slice(0, 8)}
        />
      </Suspense>
    </div>
  );
}
