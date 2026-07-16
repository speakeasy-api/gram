import { Markdown } from "@/elements/components/Markdown";
import { RequireScope } from "@/components/require-scope";
import { ErrorAlert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Dialog } from "@/components/ui/dialog";
import { Skeleton, SkeletonTable } from "@/components/ui/skeleton";
import { Type } from "@/components/ui/type";
import { useProject } from "@/contexts/Auth";
import { dateTimeFormatters, HumanizeDateTime } from "@/lib/dates";
import { useRoutes } from "@/routes";
import type { SkillVersion } from "@gram/client/models/components/skillversion.js";
import { useArchiveSkillMutation } from "@gram/client/react-query/archiveSkill.js";
import { useSkill } from "@gram/client/react-query/skill.js";
import { useSkillVersionsInfinite } from "@gram/client/react-query/skillVersions.js";
import { useQueryClient } from "@tanstack/react-query";
import { Badge, type Column, Table } from "@speakeasy-api/moonshine";
import { lazy, Suspense, useState } from "react";
import { Link, useParams } from "react-router";
import { toast } from "sonner";
import {
  SkillManifestDialog,
  type SkillManifestDialogMode,
} from "./SkillManifestDialog";
import { invalidateSkillQueries } from "./invalidate-skill-queries";
import { stripSkillFrontmatter } from "./skill-manifest";
import { SkillValidationErrors } from "./SkillValidationErrors";
import { selectDiffVersions } from "./version-selection";

const SkillTextDiff = lazy(() => import("./SkillTextDiff"));

export default function SkillDetail(): JSX.Element {
  const { skillId = "" } = useParams<{ skillId: string }>();
  const project = useProject();
  const routes = useRoutes();
  const skillQuery = useSkill({ id: skillId }, undefined, {
    enabled: skillId.length > 0,
    throwOnError: false,
  });
  const versionsQuery = useSkillVersionsInfinite({ id: skillId }, undefined, {
    enabled: skillId.length > 0,
    throwOnError: false,
  });
  const [dialogMode, setDialogMode] = useState<SkillManifestDialogMode | null>(
    null,
  );
  const [selectedVersions, setSelectedVersions] = useState<Set<string>>(
    () => new Set(),
  );

  if (skillQuery.isPending) return <SkillDetailLoading />;
  if (skillQuery.error || !skillQuery.data) {
    let error: Error | string = "The skill may have been archived or removed.";
    if (skillQuery.error instanceof Error) error = skillQuery.error;
    return <ErrorAlert title="Unable to load skill" error={error} />;
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
  let dialogInitialContent = "";
  if (dialogMode === "edit") dialogInitialContent = latestVersion.content;
  const columns = versionColumns({
    latestVersionId: latestVersion.id,
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
    <div className="space-y-8">
      <div className="flex flex-col gap-4 border-b pb-6 sm:flex-row sm:items-start sm:justify-between">
        <div className="min-w-0 space-y-2">
          <Link
            to={routes.clis.href()}
            className="text-muted-foreground text-sm hover:underline"
          >
            Skills
          </Link>
          <div className="flex flex-wrap items-center gap-2">
            <h1 className="truncate text-2xl font-semibold">
              {skill.displayName}
            </h1>
            <Badge variant="neutral">{skill.classification}</Badge>
            {!latestVersion.specValid && (
              <Badge variant="destructive">Invalid</Badge>
            )}
          </div>
          <Type muted className="font-mono">
            {skill.name}
          </Type>
          {skill.summary && <Type muted>{skill.summary}</Type>}
        </div>

        <RequireScope
          scope="skill:write"
          resourceId={project.id}
          level="component"
        >
          <div className="flex flex-wrap gap-2 sm:justify-end">
            <Button
              variant="outline"
              onClick={() => setDialogMode("add-version")}
            >
              Add version
            </Button>
            <Button onClick={() => setDialogMode("edit")}>Edit latest</Button>
            <ArchiveSkillButton
              skillId={skill.id}
              skillName={skill.displayName}
            />
          </div>
        </RequireScope>
      </div>

      {!latestVersion.specValid && (
        <ValidationErrors errors={latestVersion.validationErrors} />
      )}

      <section className="grid gap-4 md:grid-cols-2">
        <div className="border-border rounded-lg border p-4">
          <Type variant="subheading" className="mb-2">
            API description
          </Type>
          <Type small muted>
            {latestVersion.description || "No description in this version."}
          </Type>
        </div>
        <div className="border-border rounded-lg border p-4">
          <Type variant="subheading" className="mb-2">
            Metadata
          </Type>
          <pre className="bg-muted/40 max-h-48 overflow-auto rounded-md p-3 text-xs">
            {JSON.stringify(latestVersion.metadata, null, 2)}
          </pre>
        </div>
      </section>

      <section className="space-y-3">
        <h2 className="text-xl font-semibold">SKILL.md</h2>
        <div className="border-border overflow-x-auto rounded-lg border p-5 sm:p-7">
          <ManifestBody body={body} />
        </div>
      </section>

      <section className="space-y-4">
        <div>
          <h2 className="text-xl font-semibold">Version history</h2>
          <Type small muted>
            Select one older version to compare it with latest, or select any
            two loaded versions.
          </Type>
        </div>
        {versionsQuery.isPending && !versionsQuery.data && <SkeletonTable />}
        {versionsQuery.error && !versionsQuery.data && (
          <ErrorAlert title="Unable to load versions" error="Try again." />
        )}
        {versionsQuery.data && (
          <div className="overflow-x-auto">
            <Table
              columns={columns}
              data={versions}
              rowKey={(version) => version.id}
              className="min-w-[820px]"
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
      </section>

      <VersionDiff versions={diffVersions} />

      <SkillManifestDialog
        key={dialogMode ?? "closed"}
        mode={dialogMode ?? "edit"}
        open={dialogMode !== null}
        onOpenChange={(open) => {
          if (!open) setDialogMode(null);
        }}
        skillId={skill.id}
        initialContent={dialogInitialContent}
      />
    </div>
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
    <div aria-label="Loading skill" className="space-y-4">
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
  selectedVersions,
  onToggle,
}: {
  latestVersionId: string;
  selectedVersions: Set<string>;
  onToggle: (versionId: string) => void;
}): Column<SkillVersion>[] {
  return [
    {
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
    },
    {
      key: "hash",
      header: "Version",
      width: "180px",
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
      width: "170px",
      render: (version) => (
        <Type small title={dateTimeFormatters.full.format(version.createdAt)}>
          <HumanizeDateTime date={version.createdAt} />
        </Type>
      ),
    },
    {
      key: "creator",
      header: "Creator ID",
      width: "1fr",
      render: (version) => (
        <Type small muted className="font-mono">
          {version.createdByUserId}
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
    <section className="space-y-3">
      <h2 className="text-xl font-semibold">Content diff</h2>
      <Suspense fallback={<Skeleton className="h-80 w-full" />}>
        <SkillTextDiff
          oldContent={older.content}
          newContent={newer.content}
          oldLabel={older.canonicalSha256.slice(0, 8)}
          newLabel={newer.canonicalSha256.slice(0, 8)}
        />
      </Suspense>
    </section>
  );
}

export function ArchiveSkillButton({
  skillId,
  skillName,
}: {
  skillId: string;
  skillName: string;
}): JSX.Element {
  const routes = useRoutes();
  const queryClient = useQueryClient();
  const [open, setOpen] = useState(false);
  const archive = useArchiveSkillMutation();
  const [error, setError] = useState<string | null>(null);

  const archiveSkill = async (): Promise<void> => {
    setError(null);
    try {
      await archive.mutateAsync({
        request: { archiveSkillRequestBody: { id: skillId } },
      });
      routes.clis.goTo();
      await invalidateSkillQueries(queryClient);
      toast.success(`${skillName} archived`);
    } catch (archiveError) {
      let message = "Unable to archive skill.";
      if (archiveError instanceof Error) message = archiveError.message;
      setError(message);
      toast.error("Unable to archive skill");
    }
  };

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <Dialog.Trigger asChild>
        <Button variant="destructiveGhost">Archive</Button>
      </Dialog.Trigger>
      <Dialog.Content>
        <Dialog.Header>
          <Dialog.Title>Archive {skillName}?</Dialog.Title>
          <Dialog.Description>
            The skill will no longer appear in the active project registry.
          </Dialog.Description>
        </Dialog.Header>
        {error && <ErrorAlert title="Archive failed" error={error} />}
        <Dialog.Footer>
          <Button variant="outline" onClick={() => setOpen(false)}>
            Cancel
          </Button>
          <Button
            variant="destructive"
            disabled={archive.isPending}
            onClick={() => void archiveSkill()}
          >
            {archive.isPending ? "Archiving..." : "Archive skill"}
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}
