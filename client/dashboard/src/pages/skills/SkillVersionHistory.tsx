import { SettingsSection } from "@/components/detail/settings-section";
import { RequireScope } from "@/components/require-scope";
import { ErrorAlert } from "@/components/ui/Alert";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Checkbox } from "@/components/ui/Checkbox";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { useProject } from "@/contexts/Auth";
import { dateTimeFormatters, HumanizeDateTime } from "@/lib/dates";
import type { SkillVersion } from "@gram/client/models/components/skillversion.js";
import { useSkillVersionsInfinite } from "@gram/client/react-query/skillVersions.js";
import { type Column, Table } from "@/components/ui/Table";
import { lazy, Suspense, useState } from "react";
import { RestoreSkillVersionDialog } from "./RestoreSkillVersionDialog";
import { SkillValidationErrors } from "./SkillValidationErrors";
import { useSkillDetailContext } from "./SkillDetailContext";
import {
  selectDiffVersions,
  type VersionChangeDirection,
  versionChangeDirection,
} from "./version-selection";

const SkillTextDiff = lazy(() => import("./SkillTextDiff"));

export default function SkillVersionHistory(): JSX.Element {
  const { skillQueryData } = useSkillDetailContext();
  const { skill, latestVersion } = skillQueryData;
  if (!latestVersion)
    return (
      <Text small muted>
        No versions found.
      </Text>
    );
  return <VersionHistory skillId={skill.id} currentVersion={latestVersion} />;
}

function VersionHistory({
  skillId,
  currentVersion,
}: {
  skillId: string;
  currentVersion: SkillVersion;
}): JSX.Element {
  const project = useProject();
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
    <SettingsSection>
      <SettingsSection.Header>
        <SettingsSection.Title>Version history</SettingsSection.Title>
        <SettingsSection.Description>
          Versions are ordered by creation date, newest first. After a rollback,
          the current version may appear below newer versions.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <SettingsSection.Body>
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
        </SettingsSection.Body>
      </SettingsSection.Panel>
      <RestoreSkillVersionDialog
        skillId={skillId}
        version={restoreTarget?.version ?? null}
        direction={restoreTarget?.direction ?? null}
        onClose={() => setRestoreTarget(null)}
      />
    </SettingsSection>
  );
}

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
      <Suspense fallback={<div className="h-80 w-full" />}>
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
