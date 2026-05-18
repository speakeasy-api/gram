import { ProjectAvatar } from "@/components/project-menu";
import { Badge } from "@/components/ui/badge";
import { Checkbox } from "@/components/ui/checkbox";
import { Dialog } from "@/components/ui/dialog";
import { Type } from "@/components/ui/type";
import { cn } from "@/lib/utils";
import { AddServerDialog } from "@/pages/catalog/AddServerDialog";
import type { PulseMCPServer as CatalogServer } from "@/pages/catalog/hooks";
import { useRoutes } from "@/routes";
import type { ProjectEntry } from "@gram/client/models/components";
import { Button, Icon, Input } from "@speakeasy-api/moonshine";
import {
  ArrowRight,
  Circle,
  FolderOpen,
  Loader2,
  Search,
  Server,
} from "lucide-react";
import type { ChangeEvent } from "react";
import { useEffect, useMemo, useState } from "react";

type InstallResult = {
  projectId: string;
  projectSlug: string;
  projectName: string;
  status: "succeeded" | "failed";
  succeededCount: number;
  failedCount: number;
  firstCompletedToolsetSlug?: string;
  firstCompletedMcpSlug?: string;
  error?: string;
};

type ProjectInstallStatus = "pending" | "installing" | InstallResult["status"];

export function CollectionInstallDialog({
  open,
  onOpenChange,
  collectionName,
  servers,
  projects,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  collectionName: string;
  servers: CatalogServer[];
  projects: ProjectEntry[];
}) {
  const defaultProjectSlug = projects[0]?.slug;
  const [phase, setPhase] = useState<"select" | "installing" | "summary">(
    "select",
  );
  const [selectedProjectSlugs, setSelectedProjectSlugs] = useState<string[]>(
    defaultProjectSlug ? [defaultProjectSlug] : [],
  );
  const [projectSearch, setProjectSearch] = useState("");
  const [currentProjectIndex, setCurrentProjectIndex] = useState(0);
  const [results, setResults] = useState<InstallResult[]>([]);

  useEffect(() => {
    if (!open) return;

    setPhase("select");
    setProjectSearch("");
    setCurrentProjectIndex(0);
    setResults([]);
    setSelectedProjectSlugs(defaultProjectSlug ? [defaultProjectSlug] : []);
  }, [defaultProjectSlug, open]);

  const projectBySlug = useMemo(
    () => new Map(projects.map((project) => [project.slug, project])),
    [projects],
  );

  const selectedProjects = useMemo(
    () =>
      selectedProjectSlugs
        .map((slug) => projectBySlug.get(slug))
        .filter((project): project is ProjectEntry => !!project),
    [projectBySlug, selectedProjectSlugs],
  );

  const filteredProjects = useMemo(() => {
    const query = projectSearch.trim().toLowerCase();
    if (!query) return projects;
    return projects.filter(
      (project) =>
        project.name.toLowerCase().includes(query) ||
        project.slug.toLowerCase().includes(query),
    );
  }, [projectSearch, projects]);

  const currentProject = selectedProjects[currentProjectIndex];
  const successfulProjects = results.filter((r) => r.status === "succeeded");
  const failedProjects = results.filter((r) => r.status === "failed");

  const toggleProject = (slug: string) => {
    setSelectedProjectSlugs((current) =>
      current.includes(slug)
        ? current.filter((projectSlug) => projectSlug !== slug)
        : [...current, slug],
    );
  };

  const resultByProjectSlug = useMemo(
    () => new Map(results.map((result) => [result.projectSlug, result])),
    [results],
  );

  return (
    <>
      {phase === "installing" && currentProject && (
        <AddServerDialog
          key={currentProject.slug}
          servers={servers}
          projectSlug={currentProject.slug}
          open={open}
          bulk
          autoStartDeployment
          headless
          onOpenChange={(nextOpen) => {
            if (!nextOpen) {
              onOpenChange(false);
            }
          }}
          onInstallFinished={(result) => {
            const nextResult: InstallResult = {
              projectId: currentProject.id,
              projectSlug: currentProject.slug,
              projectName: currentProject.name,
              status: result.status,
              succeededCount: result.succeededCount,
              failedCount: result.failedCount,
              firstCompletedToolsetSlug: result.firstCompletedToolsetSlug,
              firstCompletedMcpSlug: result.firstCompletedMcpSlug,
              error: result.error,
            };

            setResults((current) => [...current, nextResult]);

            if (currentProjectIndex < selectedProjects.length - 1) {
              setCurrentProjectIndex((index) => index + 1);
            } else {
              setPhase("summary");
            }
          }}
        />
      )}
      <Dialog open={open} onOpenChange={onOpenChange}>
        <Dialog.Content className="sm:max-w-lg">
          {phase === "installing" ? (
            <>
              <Dialog.Header>
                <Dialog.Title>Installing collection</Dialog.Title>
                <Dialog.Description>
                  Installing {servers.length}{" "}
                  {servers.length === 1 ? "server" : "servers"} from{" "}
                  {collectionName} across {selectedProjects.length}{" "}
                  {selectedProjects.length === 1 ? "project" : "projects"}.
                </Dialog.Description>
              </Dialog.Header>
              <div className="space-y-4 py-2">
                <Type small muted>
                  Project{" "}
                  {Math.min(currentProjectIndex + 1, selectedProjects.length)}{" "}
                  of {selectedProjects.length}
                </Type>
                <ProjectProgressList
                  projects={selectedProjects}
                  resultByProjectSlug={resultByProjectSlug}
                  currentProjectSlug={currentProject?.slug}
                />
              </div>
              <Dialog.Footer>
                <Button variant="secondary" onClick={() => onOpenChange(false)}>
                  <Button.Text>Close</Button.Text>
                </Button>
              </Dialog.Footer>
            </>
          ) : phase === "summary" ? (
            <>
              <Dialog.Header>
                <Dialog.Title>Install complete</Dialog.Title>
                <Dialog.Description>
                  Installed {servers.length}{" "}
                  {servers.length === 1 ? "server" : "servers"} from{" "}
                  {collectionName} across {results.length}{" "}
                  {results.length === 1 ? "project" : "projects"}.
                </Dialog.Description>
              </Dialog.Header>
              <div className="space-y-4 py-2">
                <div className="grid grid-cols-2 gap-2">
                  <ResultStat
                    label="Succeeded"
                    value={successfulProjects.length}
                    tone="success"
                  />
                  <ResultStat
                    label="Failed"
                    value={failedProjects.length}
                    tone="danger"
                  />
                </div>
                <ProjectProgressList
                  projects={selectedProjects}
                  resultByProjectSlug={resultByProjectSlug}
                />
              </div>
              <Dialog.Footer>
                <Button onClick={() => onOpenChange(false)}>
                  <Button.Text>Close</Button.Text>
                </Button>
              </Dialog.Footer>
            </>
          ) : (
            <>
              <Dialog.Header>
                <Dialog.Title>Select projects</Dialog.Title>
                <Dialog.Description>
                  Choose where to install{" "}
                  <span className="font-medium">
                    {servers.length}{" "}
                    {servers.length === 1 ? "server" : "servers"}
                  </span>{" "}
                  from {collectionName}. Each project installs independently.
                </Dialog.Description>
              </Dialog.Header>
              <div className="space-y-4 py-2">
                <div className="rounded-lg border p-3">
                  <div className="flex items-center gap-2 text-sm font-medium">
                    <Server className="h-4 w-4" />
                    {servers.length}{" "}
                    {servers.length === 1 ? "server" : "servers"} from{" "}
                    {collectionName}
                  </div>
                </div>
                {projects.length === 0 ? (
                  <div className="flex flex-col items-center py-6 text-center">
                    <FolderOpen className="text-muted-foreground mb-2 h-8 w-8" />
                    <p className="text-muted-foreground text-sm">
                      No projects found.
                    </p>
                  </div>
                ) : (
                  <div className="space-y-3">
                    <div className="space-y-1">
                      <div className="flex items-center justify-between gap-2">
                        <label className="text-sm font-medium">Projects</label>
                        <Type small muted>
                          {selectedProjects.length} selected
                        </Type>
                      </div>
                      {selectedProjects.length > 0 && (
                        <Type small muted className="line-clamp-2">
                          {selectedProjects
                            .slice(0, 3)
                            .map((project) => project.name)
                            .join(", ")}
                          {selectedProjects.length > 3
                            ? `, +${selectedProjects.length - 3} more`
                            : ""}
                        </Type>
                      )}
                    </div>
                    {projects.length > 5 && (
                      <div className="relative">
                        <Search className="text-muted-foreground absolute top-1/2 left-3 h-4 w-4 -translate-y-1/2" />
                        <Input
                          value={projectSearch}
                          placeholder="Search projects..."
                          onChange={(e: ChangeEvent<HTMLInputElement>) =>
                            setProjectSearch(e.target.value)
                          }
                          className="pl-9"
                        />
                      </div>
                    )}
                    <div className="max-h-64 overflow-y-auto rounded-lg border">
                      {filteredProjects.length === 0 ? (
                        <div className="flex flex-col items-center py-6 text-center">
                          <Search className="text-muted-foreground mb-2 h-6 w-6" />
                          <p className="text-muted-foreground text-sm">
                            No projects match your search.
                          </p>
                        </div>
                      ) : (
                        filteredProjects.map((project) => {
                          const checked = selectedProjectSlugs.includes(
                            project.slug,
                          );
                          return (
                            <label
                              key={project.slug}
                              className="hover:bg-accent/50 flex cursor-pointer items-center gap-3 border-b px-3 py-2.5 last:border-b-0"
                            >
                              <Checkbox
                                checked={checked}
                                onCheckedChange={() =>
                                  toggleProject(project.slug)
                                }
                              />
                              <ProjectAvatar
                                project={project}
                                className="h-5 min-h-5 w-5 min-w-5"
                              />
                              <span className="min-w-0 flex-1 truncate text-sm font-medium">
                                {project.name}
                              </span>
                            </label>
                          );
                        })
                      )}
                    </div>
                  </div>
                )}
              </div>
              <Dialog.Footer>
                <Button variant="secondary" onClick={() => onOpenChange(false)}>
                  Cancel
                </Button>
                <Button
                  disabled={selectedProjects.length === 0}
                  onClick={() => {
                    setCurrentProjectIndex(0);
                    setResults([]);
                    setPhase("installing");
                  }}
                >
                  <Button.Text>
                    Install to {selectedProjects.length}{" "}
                    {selectedProjects.length === 1 ? "project" : "projects"}
                  </Button.Text>
                </Button>
              </Dialog.Footer>
            </>
          )}
        </Dialog.Content>
      </Dialog>
    </>
  );
}

function ProjectProgressList({
  projects,
  resultByProjectSlug,
  currentProjectSlug,
}: {
  projects: ProjectEntry[];
  resultByProjectSlug: Map<string, InstallResult>;
  currentProjectSlug?: string;
}) {
  return (
    <div className="max-h-64 space-y-2 overflow-y-auto">
      {projects.map((project) => {
        const result = resultByProjectSlug.get(project.slug);
        const status: ProjectInstallStatus =
          result?.status ??
          (project.slug === currentProjectSlug ? "installing" : "pending");

        return (
          <div
            key={project.slug}
            className={cn(
              "bg-card flex items-start gap-3 rounded-lg border p-3 transition-colors",
              status === "installing" && "border-primary/30 bg-primary/5",
              status === "failed" && "border-destructive/30 bg-destructive/5",
            )}
          >
            <ProjectAvatar
              project={project}
              className="mt-0.5 h-5 min-h-5 w-5 min-w-5"
            />
            <div className="min-w-0 flex-1">
              <div className="flex items-center justify-between gap-2">
                <Type className="truncate text-sm font-medium">
                  {project.name}
                </Type>
                <ProjectStatusIndicator status={status} />
              </div>
              {result && (result.status !== "succeeded" || result.error) && (
                <Type small muted className="mt-1 whitespace-pre-line">
                  {result.error ||
                    `${result.succeededCount} succeeded, ${result.failedCount} failed.`}
                </Type>
              )}
              {result?.firstCompletedToolsetSlug && (
                <ProjectConfigureLink
                  projectSlug={project.slug}
                  toolsetSlug={result.firstCompletedToolsetSlug}
                />
              )}
            </div>
          </div>
        );
      })}
    </div>
  );
}

function ResultStat({
  label,
  value,
  tone,
}: {
  label: string;
  value: number;
  tone: "success" | "danger";
}) {
  const toneClass = {
    success: "border-emerald-500/20 bg-emerald-500/10",
    danger: "border-destructive/30 bg-destructive/5",
  }[tone];

  return (
    <div className={cn("rounded-lg border p-3 text-center", toneClass)}>
      <div className="flex items-center justify-center gap-1.5">
        <StatusCircleIcon tone={tone} />
        <span className="text-lg font-semibold">{value}</span>
      </div>
      <Type small muted className="block text-center">
        {label}
      </Type>
    </div>
  );
}

function InstallResultBadge({ status }: { status: InstallResult["status"] }) {
  if (status === "succeeded") {
    return (
      <Badge variant="secondary" className="text-emerald-600">
        <StatusCircleIcon tone="success" size="sm" />
        Succeeded
      </Badge>
    );
  }

  return (
    <Badge variant="outline" className="text-destructive">
      <StatusCircleIcon tone="danger" size="sm" />
      Failed
    </Badge>
  );
}

function ProjectStatusIndicator({ status }: { status: ProjectInstallStatus }) {
  if (status === "pending") {
    return (
      <Badge variant="outline" className="text-muted-foreground">
        <Circle className="mr-1 h-3 w-3" />
        Pending
      </Badge>
    );
  }

  if (status === "installing") {
    return (
      <Badge variant="secondary" className="text-primary">
        <Loader2 className="mr-1 h-3 w-3 animate-spin" />
        Installing
      </Badge>
    );
  }

  return <InstallResultBadge status={status} />;
}

function StatusCircleIcon({
  tone,
  size = "md",
}: {
  tone: "success" | "danger";
  size?: "sm" | "md";
}) {
  return (
    <span
      className={cn(
        "inline-flex shrink-0 items-center justify-center rounded-full",
        tone === "success"
          ? "bg-success text-success-foreground"
          : "bg-destructive/20 text-destructive-foreground",
        size === "sm" ? "mr-1 h-4 w-4" : "h-5 w-5",
      )}
    >
      <Icon name={tone === "success" ? "check" : "x"} className="h-3 w-3" />
    </span>
  );
}

function ProjectConfigureLink({
  projectSlug,
  toolsetSlug,
}: {
  projectSlug: string;
  toolsetSlug: string;
}) {
  const routes = useRoutes({ projectSlug });

  return (
    <routes.mcp.details.Link
      params={[toolsetSlug]}
      className="mt-3 inline-flex no-underline hover:no-underline"
    >
      <Button variant="secondary" size="sm">
        <Button.Text>Configure MCP</Button.Text>
        <Button.RightIcon>
          <ArrowRight className="h-4 w-4" />
        </Button.RightIcon>
      </Button>
    </routes.mcp.details.Link>
  );
}
