import { DotCard } from "@/components/ui/dot-card";
import { DotRow } from "@/components/ui/dot-row";
import { DotTable } from "@/components/ui/dot-table";
import { Icon } from "@speakeasy-api/moonshine";
import { Skeleton } from "@/components/ui/skeleton";
import type { SkillEntry } from "@gram/client/models/components";
import { Type } from "@/components/ui/type";
import { ViewToggle } from "@/components/ui/view-toggle";
import { useListSkills } from "@gram/client/react-query";
import { useRoutes } from "@/routes";
import { useViewMode } from "@/components/ui/use-view-mode";

export default function SkillsRegistry() {
  const [viewMode, setViewMode] = useViewMode();
  const routes = useRoutes();
  const { data, isPending, error } = useListSkills();

  const skills = data?.skills ?? [];

  return (
    <div className="p-8">
      <div className="mx-auto max-w-6xl space-y-6">
        <div className="flex items-start justify-between gap-4">
          <div>
            <Type variant="subheading">Registry</Type>
            <Type small muted className="mt-1 block max-w-2xl">
              Browse the skills captured for this project. This is backed by the
              new `skills.list` contract. Click a row or card to open the
              skill-level view.
            </Type>
          </div>
          <ViewToggle value={viewMode} onChange={setViewMode} />
        </div>

        {error ? (
          <RegistryErrorState />
        ) : isPending ? (
          viewMode === "table" ? (
            <RegistryTableSkeleton />
          ) : (
            <RegistryGridSkeleton />
          )
        ) : skills.length === 0 ? (
          <RegistryEmptyState />
        ) : viewMode === "table" ? (
          <SkillsTable skills={skills} />
        ) : (
          <SkillsGrid skills={skills} />
        )}
      </div>
    </div>
  );
}

function SkillsGrid({ skills }: { skills: SkillEntry[] }) {
  const routes = useRoutes();

  return (
    <div className="grid grid-cols-1 gap-6 xl:grid-cols-2">
      {skills.map((skill) => (
        <DotCard
          key={skill.id}
          className="cursor-pointer"
          onClick={() => routes.skills.registry.skill.goTo(skill.slug)}
          icon={
            <Icon name="sparkles" className="text-muted-foreground h-8 w-8" />
          }
        >
          <div className="flex min-w-0 flex-1 flex-col">
            <div className="mb-1 flex items-center gap-2">
              <Type
                variant="subheading"
                className="group-hover:text-primary truncate transition-colors"
              >
                {skill.name}
              </Type>
            </div>
            {skill.description ? (
              <Type small muted className="line-clamp-2">
                {skill.description}
              </Type>
            ) : (
              <Type small muted className="italic">
                No description
              </Type>
            )}
            <div className="text-muted-foreground mt-auto flex items-center justify-between pt-3 text-xs">
              <div className="flex items-center gap-3">
                <span>
                  {skill.activeVersion?.authorName || "Unknown author"}
                </span>
                <span>{formatDateTime(skill.updatedAt)}</span>
              </div>
              <code className="font-mono">v{skill.versionCount}</code>
            </div>
          </div>
        </DotCard>
      ))}
    </div>
  );
}

function SkillsTable({ skills }: { skills: SkillEntry[] }) {
  const routes = useRoutes();

  return (
    <DotTable
      headers={[
        { label: "Name" },
        { label: "Version" },
        { label: "Author" },
        { label: "Updated" },
      ]}
    >
      {skills.map((skill) => (
        <DotRow
          key={skill.id}
          onClick={() => routes.skills.registry.skill.goTo(skill.slug)}
          icon={
            <Icon name="sparkles" className="text-muted-foreground h-5 w-5" />
          }
        >
          <td className="px-3 py-3">
            <Type
              variant="subheading"
              as="div"
              className="group-hover:text-primary truncate text-sm transition-colors"
              title={skill.name}
            >
              {skill.name}
            </Type>
            <Type small muted className="mt-0.5 block truncate">
              {skill.description || "No description"}
            </Type>
          </td>
          <td className="px-3 py-3">
            <code className="text-muted-foreground font-mono text-xs">
              v{skill.versionCount}
            </code>
          </td>
          <td className="px-3 py-3">
            <Type small muted>
              {skill.activeVersion?.authorName || "Unknown"}
            </Type>
          </td>
          <td className="px-3 py-3">
            <Type small muted>
              {formatDateTime(skill.updatedAt)}
            </Type>
          </td>
        </DotRow>
      ))}
    </DotTable>
  );
}

function RegistryEmptyState() {
  return (
    <div className="bg-muted/20 flex min-h-[360px] flex-col items-center justify-center rounded-xl border border-dashed px-8 py-24 text-center">
      <div className="bg-muted/50 mb-4 flex h-12 w-12 items-center justify-center rounded-full">
        <Icon name="sparkles" className="text-muted-foreground h-6 w-6" />
      </div>
      <Type variant="subheading" className="mb-1">
        No skills yet
      </Type>
      <Type small muted className="max-w-md">
        Skills will appear here once they are captured for this project.
      </Type>
    </div>
  );
}

function RegistryErrorState() {
  return (
    <div className="rounded-xl border border-dashed px-8 py-16 text-center">
      <Type variant="subheading" className="mb-1">
        Couldn&apos;t load skills
      </Type>
      <Type small muted>
        The registry surface is wired to the new `skills.list` endpoint, but the
        request failed.
      </Type>
    </div>
  );
}

function RegistryGridSkeleton() {
  return (
    <div className="grid grid-cols-1 gap-6 xl:grid-cols-2">
      {Array.from({ length: 4 }).map((_, index) => (
        <DotCard
          key={index}
          icon={
            <Icon name="sparkles" className="text-muted-foreground h-8 w-8" />
          }
        >
          <div className="space-y-4">
            <div className="space-y-2">
              <Skeleton className="h-5 w-40" />
              <Skeleton className="h-4 w-full" />
              <Skeleton className="h-4 w-2/3" />
            </div>
            <div className="grid grid-cols-2 gap-3">
              <Skeleton className="h-14 w-full" />
              <Skeleton className="h-14 w-full" />
              <Skeleton className="h-14 w-full" />
              <Skeleton className="h-14 w-full" />
            </div>
          </div>
        </DotCard>
      ))}
    </div>
  );
}

function RegistryTableSkeleton() {
  return (
    <DotTable
      headers={[
        { label: "Name" },
        { label: "Version" },
        { label: "Author" },
        { label: "Updated" },
      ]}
    >
      {Array.from({ length: 5 }).map((_, index) => (
        <DotRow
          key={index}
          icon={
            <Icon name="sparkles" className="text-muted-foreground h-5 w-5" />
          }
        >
          <td className="px-3 py-3">
            <div className="space-y-2">
              <Skeleton className="h-4 w-36" />
              <Skeleton className="h-3.5 w-52" />
            </div>
          </td>
          <td className="px-3 py-3">
            <Skeleton className="h-4 w-12" />
          </td>
          <td className="px-3 py-3">
            <Skeleton className="h-4 w-20" />
          </td>
          <td className="px-3 py-3">
            <Skeleton className="h-4 w-24" />
          </td>
        </DotRow>
      ))}
    </DotTable>
  );
}

function formatDateTime(date: Date) {
  return new Intl.DateTimeFormat("en-GB", {
    month: "short",
    day: "numeric",
    year: "numeric",
  }).format(date);
}
