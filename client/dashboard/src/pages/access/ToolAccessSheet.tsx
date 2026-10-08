import { InlineEmptyState } from "@/components/inline-empty-state";
import { Alert } from "@/components/ui/Alert";
import { Badge } from "@/components/ui/Badge";
import { Skeleton } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/Button";
import { Checkbox } from "@/components/ui/Checkbox";
import { RadioCard, RadioCardGroup } from "@/components/ui/RadioCard";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/Sheet";
import { useOrganization } from "@/contexts/Auth";
import { mcpServerRouteParam } from "@/lib/sources";
import { useRoutes } from "@/routes";
import type { Disposition } from "@gram/client/models/components/selector.js";
import { Check } from "lucide-react";
import { useEffect, useMemo, useState, type JSX } from "react";
import { Link } from "react-router";

import { ServerMark } from "./McpAccessParts";
import { LIST_FRAME, LIST_ROW, LIST_ROW_SELECTED } from "./mcpAccessStyles";
import {
  convertToolLimit,
  DISPOSITION_COPY,
  DISPOSITIONS,
  toolDisposition,
  type ServerWithProject,
  type ToolLimit,
} from "./mcpAccessModel";
import { useServerTools, type ToolSource } from "./useServerTools";
import type { ServerTool } from "./serverMerge";

export type ToolAccessTarget =
  | { kind: "all" }
  | { kind: "server"; entry: ServerWithProject };

export type ToolSheetTab = "tools" | "annotations";

/** Edit one server's tool access, or the tool access of All servers. */
export function ToolAccessSheet({
  target,
  limit,
  initialTab,
  applyTab = false,
  onChange,
  onClose,
}: {
  target: ToolAccessTarget | null;
  limit: ToolLimit;
  initialTab: ToolSheetTab;
  /**
   * Limit the server the way `initialTab` says as soon as its tools are known,
   * as picking "Edit by tool…" or "Edit by annotation…" asks.
   */
  applyTab?: boolean;
  onChange: (limit: ToolLimit) => void;
  onClose: () => void;
}): JSX.Element {
  return (
    <Sheet
      open={!!target}
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <SheetContent
        side="right"
        className="flex w-full flex-col gap-0 sm:max-w-md"
      >
        {target && (
          <ToolAccessSheetBody
            key={target.kind === "server" ? target.entry.server.id : "*"}
            target={target}
            limit={limit}
            initialTab={initialTab}
            applyTab={applyTab}
            onChange={onChange}
            onClose={onClose}
          />
        )}
      </SheetContent>
    </Sheet>
  );
}

function ToolAccessSheetBody({
  target,
  limit,
  initialTab,
  applyTab,
  onChange,
  onClose,
}: {
  target: ToolAccessTarget;
  limit: ToolLimit;
  initialTab: ToolSheetTab;
  applyTab: boolean;
  onChange: (limit: ToolLimit) => void;
  onClose: () => void;
}): JSX.Element {
  const isAllServers = target.kind === "all";
  const entry = target.kind === "server" ? target.entry : undefined;
  // All servers has no tool list, so it is limited by annotation only.
  const [tab, setTab] = useState<ToolSheetTab>(
    isAllServers ? "annotations" : initialTab,
  );
  const source = useServerTools(entry);
  const readyTools = source.status === "ready" ? source.tools : undefined;
  const tools = useMemo(() => readyTools ?? [], [readyTools]);
  const specific = limit.kind !== "all";

  // A remote server's tools arrive after the sheet opens, and converting
  // before then would carry nothing over.
  const [pendingApply, setPendingApply] = useState(applyTab);
  useEffect(() => {
    if (!pendingApply || source.status === "loading") return;
    setPendingApply(false);
    onChange(convertToolLimit(limit, tab, tools));
  }, [pendingApply, source.status, limit, tab, tools, onChange]);

  const chooseTab = (next: ToolSheetTab) => {
    setTab(next);
    onChange(convertToolLimit(limit, next, tools));
  };

  return (
    <>
      <SheetHeader className="border-border border-b">
        <div className="flex items-center gap-3 pr-6">
          {entry && <ServerMark />}
          <div className="min-w-0">
            <p className="text-eyebrow">
              Tool access · {entry ? entry.projectName : "Every project"}
            </p>
            <SheetTitle className="truncate">
              {entry ? entry.server.name : "All servers"}
            </SheetTitle>
          </div>
        </div>
        <SheetDescription className="sr-only">
          Choose which tools members of this role can call.
        </SheetDescription>
      </SheetHeader>

      <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto p-4">
        <fieldset className="flex flex-col gap-2">
          <legend className="mb-2 text-sm font-medium">
            Which tools can members call?
          </legend>
          <RadioCardGroup
            size="sm"
            value={specific ? "specific" : "all"}
            onValueChange={(value) =>
              onChange(
                value === "all"
                  ? { kind: "all" }
                  : convertToolLimit(limit, tab, tools),
              )
            }
          >
            <RadioCard value="all" title="All tools">
              Every tool, including ones added later.
            </RadioCard>
            <RadioCard value="specific" title="Specific tools">
              {isAllServers
                ? "Allow tools by their annotation."
                : "Pick tools, or allow them by annotation."}
            </RadioCard>
          </RadioCardGroup>
        </fieldset>

        {specific && !isAllServers && (
          <SegmentedControl
            className="self-start"
            value={tab}
            onChange={chooseTab}
            options={[
              { value: "tools", label: "By tool" },
              { value: "annotations", label: "By annotation" },
            ]}
          />
        )}

        {specific && tab === "tools" && limit.kind === "tools" && entry && (
          <ToolChecklist
            entry={entry}
            source={source}
            selected={limit.tools}
            onChange={(names) => onChange({ kind: "tools", tools: names })}
          />
        )}
        {specific && tab === "annotations" && limit.kind === "annotations" && (
          <AnnotationChecklist
            tools={entry && source.status === "ready" ? tools : undefined}
            selected={limit.dispositions}
            onChange={(dispositions) =>
              onChange({ kind: "annotations", dispositions })
            }
          />
        )}
      </div>

      <SheetFooter className="border-border flex-row justify-end border-t">
        <Button variant="primary" onClick={onClose}>
          <Button.LeftIcon>
            <Check className="h-4 w-4" />
          </Button.LeftIcon>
          <Button.Text>Done</Button.Text>
        </Button>
      </SheetFooter>
    </>
  );
}

function ToolChecklist({
  entry,
  source,
  selected,
  onChange,
}: {
  entry: ServerWithProject;
  source: ToolSource;
  selected: string[];
  onChange: (tools: string[]) => void;
}): JSX.Element {
  switch (source.status) {
    case "loading":
      return (
        <Skeleton>
          <div className="h-9 w-full" />
          <div className="h-9 w-full" />
          <div className="h-9 w-full" />
        </Skeleton>
      );
    case "error":
      return (
        <Alert variant="error" alignTop className="text-sm">
          <span className="flex flex-wrap items-center gap-2">
            Couldn&rsquo;t load this server&rsquo;s tools.
            <Button variant="tertiary" size="xs" onClick={source.retry}>
              <Button.Text>Retry</Button.Text>
            </Button>
          </span>
        </Alert>
      );
    case "dynamic":
    case "none":
      return (
        <Alert variant="default" alignTop className="text-sm">
          This server resolves its tools when they&rsquo;re called, so it
          can&rsquo;t be limited by tool. Limit it by annotation instead.
        </Alert>
      );
    case "needs-connect":
      return <ConnectPrompt entry={entry} connect={source.connect} />;
    case "ready":
      break;
  }

  const chosen = new Set(selected);
  const { tools } = source;
  const picked = tools.filter((tool) => chosen.has(tool.name)).length;
  const toggle = (name: string, on: boolean) =>
    onChange(
      on ? [...selected, name] : selected.filter((tool) => tool !== name),
    );

  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center gap-2">
        <Text as="span" muted small className="mr-auto">
          {picked} of {tools.length} tools selected
        </Text>
        <Button
          variant="tertiary"
          size="xs"
          onClick={() => onChange(tools.map((tool) => tool.name))}
        >
          <Button.Text>Select all</Button.Text>
        </Button>
        <Button variant="tertiary" size="xs" onClick={() => onChange([])}>
          <Button.Text>Clear</Button.Text>
        </Button>
      </div>
      <div className={LIST_FRAME}>
        {tools.map((tool) => {
          const disposition = toolDisposition(tool);
          return (
            <label
              key={tool.id}
              className={cn(
                LIST_ROW,
                "cursor-pointer",
                chosen.has(tool.name) && LIST_ROW_SELECTED,
              )}
            >
              <Checkbox
                checked={chosen.has(tool.name)}
                onCheckedChange={(next) => toggle(tool.name, next === true)}
              />
              <Text as="span" mono small className="min-w-0 flex-1 truncate">
                {tool.name}
              </Text>
              {disposition && (
                <Badge variant="neutral" size="sm">
                  {DISPOSITION_COPY[disposition].label}
                </Badge>
              )}
            </label>
          );
        })}
      </div>
    </div>
  );
}

function AnnotationChecklist({
  tools,
  selected,
  onChange,
}: {
  /** The server's tools, to count per annotation; absent for All servers. */
  tools: ServerTool[] | undefined;
  selected: Disposition[];
  onChange: (dispositions: Disposition[]) => void;
}): JSX.Element {
  const toggle = (disposition: Disposition, on: boolean) =>
    onChange(
      DISPOSITIONS.filter((d) =>
        d === disposition ? on : selected.includes(d),
      ),
    );
  return (
    <div className={LIST_FRAME}>
      {DISPOSITIONS.map((disposition) => {
        const copy = DISPOSITION_COPY[disposition];
        const count = tools?.filter(
          (tool) => toolDisposition(tool) === disposition,
        ).length;
        return (
          <label
            key={disposition}
            className={cn(
              LIST_ROW,
              "cursor-pointer",
              selected.includes(disposition) && LIST_ROW_SELECTED,
            )}
          >
            <Checkbox
              checked={selected.includes(disposition)}
              onCheckedChange={(next) => toggle(disposition, next === true)}
            />
            <span className="flex min-w-0 flex-1 flex-col">
              <Text as="span" className="text-sm">
                {copy.label}
              </Text>
              <Text as="span" muted small>
                {copy.description}
              </Text>
            </span>
            {count !== undefined && (
              <Badge variant="neutral" size="sm">
                {count} {count === 1 ? "tool" : "tools"}
              </Badge>
            )}
          </label>
        );
      })}
    </div>
  );
}

/**
 * A remote server lists its tools once you have an upstream session with it.
 * Connect signs in on the server's connect page in a new tab, so unsaved
 * changes to the role survive; coming back here lists and records the tools.
 * A server with no connect page points at its settings instead.
 */
function ConnectPrompt({
  entry,
  connect,
}: {
  entry: ServerWithProject;
  connect: (() => void) | undefined;
}): JSX.Element {
  const organization = useOrganization();
  const projectSlug = organization.projects.find(
    (p) => p.id === entry.projectId,
  )?.slug;
  const routes = useRoutes({ projectSlug });
  if (connect) {
    return (
      <InlineEmptyState
        icon="plug-zap"
        heading="You must connect in order to permission by tool"
        description="Sign in to this server and its tools are listed here."
        action={
          <Button variant="secondary" size="sm" onClick={connect}>
            <Button.Text>Connect</Button.Text>
          </Button>
        }
      />
    );
  }
  return (
    <InlineEmptyState
      icon="plug-zap"
      heading="This server's tools can't be listed yet"
      description="Connect isn't available for this server. Review its authentication settings."
      action={
        <Button variant="secondary" size="sm" asChild>
          <Link
            to={routes.mcp.x.settings.href(
              mcpServerRouteParam({
                id: entry.server.id,
                slug: entry.server.slug,
              }),
            )}
            target="_blank"
            rel="noopener noreferrer"
          >
            <Button.Text>Open settings</Button.Text>
          </Link>
        </Button>
      }
    />
  );
}
