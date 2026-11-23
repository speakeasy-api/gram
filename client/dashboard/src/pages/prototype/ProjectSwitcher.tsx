import { useIdentityTint } from "@/components/gradient-colors";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/Dropdown";
import { Icon } from "@/components/ui/Icon";
import { useOrganization, useProject } from "@/contexts/Auth";
import { useSlugs } from "@/contexts/Sdk";
import { useNavigate } from "react-router";

function ProjectTile({ name }: { name: string }): JSX.Element {
  const tint = useIdentityTint(name);
  return (
    <span
      aria-hidden="true"
      style={tint}
      className="flex size-5 shrink-0 items-center justify-center border text-[11px]"
    >
      {name.charAt(0).toUpperCase()}
    </span>
  );
}

/**
 * Switches the prototype to another project in the organization. The
 * prototype stays the prototype: switching keeps you on this route.
 */
export function ProjectSwitcher(): JSX.Element {
  const project = useProject();
  const organization = useOrganization();
  const { orgSlug } = useSlugs();
  const navigate = useNavigate();
  const projects = [...organization.projects].sort((a, b) =>
    a.name.localeCompare(b.name),
  );

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <button
          type="button"
          className="hover:bg-surface-tertiary-default flex items-center gap-2 px-2 py-1 text-sm transition-colors"
        >
          <ProjectTile name={project.name} />
          <span className="max-w-[160px] truncate">{project.name}</span>
          <Icon
            name="chevrons-up-down"
            className="text-muted-foreground size-3.5"
            aria-hidden="true"
          />
          <span className="sr-only">Switch project</span>
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent
        align="start"
        className="max-h-80 w-64 overflow-y-auto"
      >
        <DropdownMenuLabel className="text-eyebrow">Projects</DropdownMenuLabel>
        <DropdownMenuSeparator />
        {projects.map((p) => (
          <DropdownMenuItem
            key={p.id}
            onSelect={() => {
              if (p.slug !== project.slug) {
                void navigate(`/${orgSlug}/projects/${p.slug}/prototype`);
              }
            }}
            className="flex items-center gap-2"
          >
            <ProjectTile name={p.name} />
            <span className="min-w-0 flex-1 truncate">{p.name}</span>
            {p.slug === project.slug && (
              <Icon name="check" className="size-3.5" aria-hidden="true" />
            )}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
