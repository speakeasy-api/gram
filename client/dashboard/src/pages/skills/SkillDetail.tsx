import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import {
  RouteNotFoundState,
  SecondaryRouteAction,
} from "@/components/route-not-found-state";
import { ErrorAlert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Skeleton, SkeletonTable } from "@/components/ui/skeleton";
import { Type } from "@/components/ui/type";
import { useProject } from "@/contexts/Auth";
import { Markdown } from "@/elements/components/Markdown";
import { dateTimeFormatters, HumanizeDateTime } from "@/lib/dates";
import { isNotFoundError } from "@/lib/route-errors";
import {
  DangerSettingsSection,
  SettingsSection,
} from "@/pages/mcp/x/tabs/settings/SettingsSection";
import { useRoutes } from "@/routes";
import type { SkillVersion } from "@gram/client/models/components/skillversion.js";
import { useSkill } from "@gram/client/react-query/skill.js";
import { useSkillVersionsInfinite } from "@gram/client/react-query/skillVersions.js";
import { Badge, type Column, Table } from "@speakeasy-api/moonshine";
import { lazy, Suspense, useEffect, useState } from "react";
import { useLocation, useNavigate, useParams } from "react-router";
import {
  ArchiveSkillDialog,
  type ArchiveSkillTarget,
} from "./ArchiveSkillDialog";
import { SkillDistributionsSection } from "./SkillDistributionsSection";
import { stripSkillFrontmatter } from "./skill-manifest";
import { SkillManifestDialog } from "./SkillManifestDialog";
import { SkillPluginBanner } from "./SkillPluginBanner";
import { SkillValidationErrors } from "./SkillValidationErrors";
import { selectDiffVersions } from "./version-selection";

const SkillTextDiff = lazy(() => import("./SkillTextDiff"));

export const SKILL_MANIFEST_SECTION_ID = "manifest";
export const SKILL_FRONTMATTER_SECTION_ID = "frontmatter";
export const SKILL_DISTRIBUTIONS_SECTION_ID = "distributions";
export const SKILL_VERSIONS_SECTION_ID = "versions";
const SKILL_DANGER_SECTION_ID = "danger";

const SKILL_SECTION_IDS: readonly string[] = [
  SKILL_MANIFEST_SECTION_ID,
  SKILL_FRONTMATTER_SECTION_ID,
  SKILL_DISTRIBUTIONS_SECTION_ID,
  SKILL_VERSIONS_SECTION_ID,
  SKILL_DANGER_SECTION_ID,
];

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
  const [archiveTarget, setArchiveTarget] = useState<ArchiveSkillTarget | null>(
    null,
  );
  useScrollToSectionHash();

  const { skill, latestVersion } = skillQueryData;
  const body = stripSkillFrontmatter(latestVersion.content);
  const frontmatterEntries = Object.entries(
    latestVersion.frontmatter ?? {},
  ).filter(([key]) => key !== "name" && key !== "description");

  return (
    <>
      <SkillPluginBanner skillId={skillId} />

      <SettingsSection id={SKILL_MANIFEST_SECTION_ID}>
        <SettingsSection.Header>
          <SettingsSection.Title>SKILL.md</SettingsSection.Title>
          <SettingsSection.Description>
            The latest version of this skill's manifest, exactly as agents load
            it.
          </SettingsSection.Description>
        </SettingsSection.Header>
        <SettingsSection.Panel>
          <SettingsSection.Body>
            {!latestVersion.specValid && (
              <ValidationErrors errors={latestVersion.validationErrors} />
            )}
            <div className="overflow-x-auto">
              <ManifestBody body={body} />
            </div>
          </SettingsSection.Body>
          <SettingsSection.Footer>
            <SettingsSection.FooterHint>
              Latest version{" "}
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
                  Edit skill
                </Button>
              </RequireScope>
            </SettingsSection.FooterActions>
          </SettingsSection.Footer>
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

      <SettingsSection id={SKILL_DISTRIBUTIONS_SECTION_ID}>
        <SettingsSection.Header>
          <SettingsSection.Title>Plugin distributions</SettingsSection.Title>
          <SettingsSection.Description>
            Used by {skillQueryData.assistantCount}{" "}
            {skillQueryData.assistantCount === 1 ? "assistant" : "assistants"}.
            The plugins carrying this skill ship it inside the plugin package
            for everyone who installs it.
          </SettingsSection.Description>
        </SettingsSection.Header>
        <SkillDistributionsSection skillId={skillId} />
      </SettingsSection>

      <SettingsSection id={SKILL_VERSIONS_SECTION_ID}>
        <SettingsSection.Header>
          <SettingsSection.Title>Version history</SettingsSection.Title>
          <SettingsSection.Description>
            Every recorded version of this skill's manifest.
          </SettingsSection.Description>
        </SettingsSection.Header>
        <VersionHistory skillId={skillId} latestVersionId={latestVersion.id} />
      </SettingsSection>

      <DangerSettingsSection id={SKILL_DANGER_SECTION_ID}>
        <DangerSettingsSection.Header>
          <DangerSettingsSection.Title>Danger zone</DangerSettingsSection.Title>
        </DangerSettingsSection.Header>
        <DangerSettingsSection.Panel>
          <DangerSettingsSection.Body className="flex flex-wrap items-center justify-between gap-4">
            <div className="space-y-1">
              <Type className="text-sm font-semibold">Archive this skill</Type>
              <Type small muted className="max-w-xl">
                Archiving removes the skill from this project's catalog and
                revokes its plugin distributions.
              </Type>
            </div>
            <RequireScope
              scope="skill:write"
              resourceId={project.id}
              level="component"
            >
              <Button
                variant="destructiveGhost"
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

      <SkillManifestDialog
        key={editOpen ? "edit" : "closed"}
        mode="edit"
        open={editOpen}
        onOpenChange={setEditOpen}
        skillId={skill.id}
        initialContent={latestVersion.content}
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
  latestVersionId,
}: {
  skillId: string;
  latestVersionId: string;
}): JSX.Element {
  const versionsQuery = useSkillVersionsInfinite({ id: skillId }, undefined, {
    throwOnError: false,
  });
  const [selectedVersions, setSelectedVersions] = useState<Set<string>>(
    () => new Set(),
  );

  const versions =
    versionsQuery.data?.pages.flatMap((page) => page.result.versions) ?? [];
  const diffVersions = selectDiffVersions(
    versions,
    selectedVersions,
    latestVersionId,
  );
  const comparable = versions.length > 1;
  let loadMoreLabel = "Load more versions";
  if (versionsQuery.isFetchingNextPage) loadMoreLabel = "Loading...";
  const columns = versionColumns({
    latestVersionId,
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
  });

  return (
    <SettingsSection.Panel>
      <SettingsSection.Body>
        {comparable && (
          <Type small muted>
            Select one older version to compare it with latest, or select any
            two loaded versions.
          </Type>
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
            variant="outline"
            disabled={versionsQuery.isFetchingNextPage}
            onClick={() => void versionsQuery.fetchNextPage()}
          >
            {loadMoreLabel}
          </Button>
        )}
        <VersionDiff versions={diffVersions} />
      </SettingsSection.Body>
    </SettingsSection.Panel>
  );
}

function ManifestBody({ body }: { body: string }): JSX.Element {
  if (body.trim().length === 0) {
    return (
      <Type small muted>
        This manifest has no Markdown body.
      </Type>
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
          <Skeleton className="h-36 w-full rounded-xl" />
          <Skeleton className="h-80 w-full rounded-xl" />
          <Skeleton className="h-48 w-full rounded-xl" />
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
    <div className="border-destructive/40 bg-destructive/5 rounded-lg border p-4">
      <Type variant="subheading" className="text-destructive mb-2">
        Latest version has validation issues
      </Type>
      <SkillValidationErrors errors={errors} />
    </div>
  );
}

function versionColumns({
  latestVersionId,
  comparable,
  selectedVersions,
  onToggle,
}: {
  latestVersionId: string;
  comparable: boolean;
  selectedVersions: Set<string>;
  onToggle: (versionId: string) => void;
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
      width: "160px",
      render: (version) => (
        <div className="flex items-center gap-2">
          <span className="font-mono text-sm">
            {version.canonicalSha256.slice(0, 8)}
          </span>
          {version.id === latestVersionId && (
            <Badge variant="information">Latest</Badge>
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
      key: "created",
      header: "Created",
      width: "150px",
      render: (version) => (
        <Type small title={dateTimeFormatters.full.format(version.createdAt)}>
          <HumanizeDateTime date={version.createdAt} />
        </Type>
      ),
    },
  ];
}

function LoadMoreError({ onRetry }: { onRetry: () => void }): JSX.Element {
  return (
    <div className="border-destructive/40 bg-destructive/5 flex flex-wrap items-center justify-between gap-3 rounded-lg border p-3">
      <Type small className="text-destructive">
        Unable to load more versions.
      </Type>
      <Button size="sm" variant="outline" onClick={onRetry}>
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
      <Type small muted mono className="text-xs tracking-wider">
        Diff · {older.canonicalSha256.slice(0, 8)} →{" "}
        {newer.canonicalSha256.slice(0, 8)}
      </Type>
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
