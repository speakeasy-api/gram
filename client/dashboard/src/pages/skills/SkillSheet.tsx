import { RequireScope } from "@/components/require-scope";
import { ErrorAlert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { Skeleton, SkeletonTable } from "@/components/ui/skeleton";
import { Type } from "@/components/ui/type";
import { useProject } from "@/contexts/Auth";
import { Markdown } from "@/elements/components/Markdown";
import { dateTimeFormatters, HumanizeDateTime } from "@/lib/dates";
import type { SkillVersion } from "@gram/client/models/components/skillversion.js";
import { useSkill } from "@gram/client/react-query/skill.js";
import { useSkillVersionsInfinite } from "@gram/client/react-query/skillVersions.js";
import { Badge, type Column, Icon, Table } from "@speakeasy-api/moonshine";
import { lazy, Suspense, useState, type ReactNode } from "react";
import {
  ArchiveSkillDialog,
  type ArchiveSkillTarget,
} from "./ArchiveSkillDialog";
import { stripSkillFrontmatter } from "./skill-manifest";
import { SkillManifestDialog } from "./SkillManifestDialog";
import { SkillValidationErrors } from "./SkillValidationErrors";
import { selectDiffVersions } from "./version-selection";

const SkillTextDiff = lazy(() => import("./SkillTextDiff"));

export function SkillSheet({
  skillId,
  onOpenChange,
}: {
  skillId: string | null;
  onOpenChange: (open: boolean) => void;
}): JSX.Element {
  return (
    <Sheet open={skillId !== null} onOpenChange={onOpenChange}>
      <SheetContent className="w-full gap-0 overflow-y-auto sm:max-w-3xl">
        {skillId !== null && (
          <SkillSheetContent
            key={skillId}
            skillId={skillId}
            onOpenChange={onOpenChange}
          />
        )}
      </SheetContent>
    </Sheet>
  );
}

function SkillSheetContent({
  skillId,
  onOpenChange,
}: {
  skillId: string;
  onOpenChange: (open: boolean) => void;
}): JSX.Element {
  const project = useProject();
  const skillQuery = useSkill({ id: skillId }, undefined, {
    throwOnError: false,
  });
  const versionsQuery = useSkillVersionsInfinite({ id: skillId }, undefined, {
    throwOnError: false,
  });
  const [editOpen, setEditOpen] = useState(false);
  const [archiveTarget, setArchiveTarget] = useState<ArchiveSkillTarget | null>(
    null,
  );
  const [selectedVersions, setSelectedVersions] = useState<Set<string>>(
    () => new Set(),
  );

  if (skillQuery.isPending) return <SkillSheetLoading />;
  if (skillQuery.error || !skillQuery.data) {
    let error: Error | string = "The skill may have been archived or removed.";
    if (skillQuery.error instanceof Error) error = skillQuery.error;
    return (
      <div className="p-6">
        <SheetTitle className="sr-only">Skill</SheetTitle>
        <ErrorAlert title="Unable to load skill" error={error} />
      </div>
    );
  }

  const { skill, latestVersion } = skillQuery.data;
  const versions =
    versionsQuery.data?.pages.flatMap((page) => page.result.versions) ?? [];
  const diffVersions = selectDiffVersions(
    versions,
    selectedVersions,
    latestVersion.id,
  );
  const body = stripSkillFrontmatter(latestVersion.content);
  let loadMoreLabel = "Load more versions";
  if (versionsQuery.isFetchingNextPage) loadMoreLabel = "Loading...";
  const comparable = versions.length > 1;
  const columns = versionColumns({
    latestVersionId: latestVersion.id,
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
  const frontmatterEntries = Object.entries(
    latestVersion.frontmatter ?? {},
  ).filter(([key]) => key !== "name" && key !== "description");

  return (
    <>
      <SheetHeader className="border-b p-6">
        <div className="flex flex-wrap items-center gap-2">
          <SheetTitle className="truncate font-mono text-2xl">
            {skill.name}
          </SheetTitle>
          <Badge variant="neutral">{skill.classification}</Badge>
          {!latestVersion.specValid && (
            <Badge variant="destructive">Needs review</Badge>
          )}
        </div>
        <Type small muted className="text-xs" mono>
          {skill.sourceKind} · {skill.versionCount}{" "}
          {skill.versionCount === 1 ? "version" : "versions"} · updated{" "}
          <HumanizeDateTime date={skill.updatedAt} />
        </Type>
        <SheetDescription className="mt-2">
          {skill.summary || "No summary provided."}
        </SheetDescription>

        <RequireScope
          scope="skill:write"
          resourceId={project.id}
          level="component"
        >
          <div className="mt-3 flex flex-wrap gap-2">
            <Button onClick={() => setEditOpen(true)}>Edit skill</Button>
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
          </div>
        </RequireScope>
      </SheetHeader>

      <div className="space-y-6 p-6">
        {!latestVersion.specValid && (
          <ValidationErrors errors={latestVersion.validationErrors} />
        )}

        <SheetSection label="SKILL.md · latest version">
          <div className="border-border overflow-x-auto rounded-lg border p-5">
            <ManifestBody body={body} />
          </div>
        </SheetSection>

        {frontmatterEntries.length > 0 && (
          <SheetSection label="Frontmatter">
            <div className="border-border rounded-lg border p-4">
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
            </div>
          </SheetSection>
        )}

        <SheetSection label="Version history">
          <div className="space-y-4">
            {comparable && (
              <Type small muted>
                Select one older version to compare it with latest, or select
                any two loaded versions.
              </Type>
            )}
            {versionsQuery.isPending && !versionsQuery.data && (
              <SkeletonTable />
            )}
            {versionsQuery.error && !versionsQuery.data && (
              <ErrorAlert title="Unable to load versions" error="Try again." />
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
              <LoadMoreError
                onRetry={() => void versionsQuery.fetchNextPage()}
              />
            )}
            {versionsQuery.hasNextPage &&
              !versionsQuery.isFetchNextPageError && (
                <Button
                  variant="outline"
                  disabled={versionsQuery.isFetchingNextPage}
                  onClick={() => void versionsQuery.fetchNextPage()}
                >
                  {loadMoreLabel}
                </Button>
              )}
            <VersionDiff versions={diffVersions} />
          </div>
        </SheetSection>
      </div>

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
        onArchived={() => onOpenChange(false)}
      />
    </>
  );
}

function SheetSection({
  label,
  children,
}: {
  label: string;
  children: ReactNode;
}): JSX.Element {
  return (
    <Collapsible defaultOpen asChild>
      <section className="space-y-3">
        <CollapsibleTrigger className="group flex w-full items-center gap-2 text-left">
          <Icon
            name="chevron-down"
            className="text-muted-foreground h-3.5 w-3.5 transition-transform group-data-[state=closed]:-rotate-90"
          />
          <span className="text-muted-foreground font-mono text-xs tracking-wider">
            {label}
          </span>
        </CollapsibleTrigger>
        <CollapsibleContent>{children}</CollapsibleContent>
      </section>
    </Collapsible>
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

function SkillSheetLoading(): JSX.Element {
  return (
    <div aria-label="Loading skill" className="space-y-4 p-6">
      <SheetTitle className="sr-only">Loading skill</SheetTitle>
      <Skeleton className="h-8 w-72" />
      <Skeleton className="h-4 w-96 max-w-full" />
      <Skeleton className="h-80 w-full" />
    </div>
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
