import { Outlet, useOutletContext, useParams } from "react-router";
import { PageTabsTrigger, Tabs, TabsList } from "@/components/ui/tabs";

import { Button } from "@/components/ui/button";
import { DownloadIcon } from "lucide-react";
import { Icon } from "@speakeasy-api/moonshine";
import { Link } from "react-router";
import type { SkillEntry } from "@gram/client/models/components";
import { SkillsPlaceholder } from "./SkillsPlaceholder";
import { Type } from "@/components/ui/type";
import { useListSkills } from "@gram/client/react-query";
import { useRoutes } from "@/routes";

type SkillDetailContext = {
  skill: SkillEntry;
};

export function SkillDetailRoot() {
  const routes = useRoutes();
  const { skillSlug } = useParams();
  const { data, isPending, error } = useListSkills(undefined, undefined, {
    enabled: Boolean(skillSlug),
  });

  const skill = data?.skills.find((entry) => entry.slug === skillSlug) ?? null;

  const routeParam = skill?.slug ?? skillSlug ?? "";
  const activeTab = routes.skills.registry.skill.versions.active
    ? "versions"
    : routes.skills.registry.skill.activity.active
      ? "activity"
      : routes.skills.registry.skill.install.active
        ? "install"
        : "definition";

  if (isPending) {
    return (
      <SkillDetailState
        title="Loading skill"
        description="Fetching skill details from the registry list."
      />
    );
  }

  if (error || !skill) {
    return (
      <SkillDetailState
        title="Skill not found"
        description="The selected skill could not be resolved from the current registry response."
      />
    );
  }

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
          <Button
            size="sm"
            variant="ghost"
            disabled
            title="Download not implemented yet"
          >
            <DownloadIcon className="h-4 w-4" />
          </Button>
        </div>
        <Type small muted className="block">
          {skill.description ||
            "No description has been set for this skill yet."}
        </Type>
        <div className="text-muted-foreground flex items-center gap-4 text-xs">
          {skill.activeVersion?.authorName && (
            <span>by {skill.activeVersion.authorName}</span>
          )}
          <span>Updated {formatDate(skill.updatedAt)}</span>
          <span>
            v{skill.versionCount} · {skill.versionCount} version
            {skill.versionCount === 1 ? "" : "s"}
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

          <Outlet context={{ skill }} />
        </Tabs>
      </div>
    </div>
  );
}

export function SkillDefinitionPage() {
  const { skill } = useSkillDetail();

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
          {skill.activeVersion && (
            <Type small muted className="font-mono">
              v{skill.versionCount}
            </Type>
          )}
        </div>
        <div className="mt-4 space-y-3">
          <DefinitionBlock
            label="Author"
            value={skill.activeVersion?.authorName || "Unknown"}
          />
          <DefinitionBlock
            label="Format"
            value={skill.activeVersion?.assetFormat || "N/A"}
          />
          <DefinitionBlock
            label="Size"
            value={
              skill.activeVersion
                ? formatBytes(skill.activeVersion.sizeBytes)
                : "N/A"
            }
          />
          <DefinitionBlock
            label="First seen"
            value={
              skill.activeVersion?.firstSeenAt
                ? formatDate(skill.activeVersion.firstSeenAt)
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
  return <SkillsPlaceholder title="Versions" description="Not implemented" />;
}

export function SkillActivityPage() {
  return <SkillsPlaceholder title="Activity" description="Not implemented" />;
}

export function SkillInstallPage() {
  return <SkillsPlaceholder title="Install" description="Not implemented" />;
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

function formatBytes(bytes: number) {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${Math.round(bytes / 1024)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

function formatDate(date: Date) {
  return new Intl.DateTimeFormat("en-GB", {
    month: "short",
    day: "numeric",
    year: "numeric",
  }).format(date);
}
