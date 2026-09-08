import { AccessListRow, InlineChoice } from "@/components/access/AccessListRow";
import { IdentityLink } from "@/components/identity-link";
import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/Button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/Dropdown";
import { Heading } from "@/components/ui/Heading";
import { SkeletonTable } from "@/components/ui/Skeleton";
import {
  PageTabsList,
  PageTabsTrigger,
  Tabs,
  TabsContent,
} from "@/components/ui/Tabs";
import { Text } from "@/components/ui/Text";
import { useOrgRoutes } from "@/routes";
import { useNavigate } from "react-router";
import type { ResourceAudienceEntry } from "@gram/client/models/components/resourceaudienceentry.js";
import type { SetResourceAudienceEntry } from "@gram/client/models/components/setresourceaudienceentry.js";
import { invalidateAllResourceAudience } from "@gram/client/react-query/resourceAudience.js";
import { useSetResourceAudienceMutation } from "@gram/client/react-query/setResourceAudience.js";
import { useQueryClient } from "@tanstack/react-query";
import { ChevronLeft, ChevronRight, Plus } from "lucide-react";
import { useMemo, useState, type JSX } from "react";
import { toast } from "sonner";
import { AddAudienceDialog } from "./AddAudienceDialog";
import type { ToolSelectionTool } from "@/components/tool-selection/ToolSelectionPanel";
import { ToolNarrowingDialog } from "./ToolNarrowingDialog";
import {
  ACCESS_PAGE_SIZE,
  pageCount,
  pageOf,
  withAdded,
  withLevel,
  withNarrowing,
  withoutPrincipals,
} from "./manageAccessState";
import {
  narrowingLabel,
  GRANTABLE_LEVELS,
  LEVEL_DESCRIPTION,
  LEVEL_LABEL,
  LEVEL_MENU_LABEL,
  LEVEL_VERB,
  inheritedRules,
  ownRules,
  type AudienceLevel,
} from "./serverAudience";

type AddTarget = "people" | "groups" | null;

/**
 * Manage access for one resource: the rules naming it, editable in place, and
 * the organization-wide rules it inherits. Direct rules are the only ones this
 * surface writes, so the two tabs mirror the two places a rule can be owned.
 */
export function ManageAccess({
  resourceId,
  resourceName,
  entries,
  toolCatalog,
  isLoading,
}: {
  resourceId: string;
  resourceName?: string;
  entries: ResourceAudienceEntry[];
  /** The server's tools, when this backend exposes a catalogue. */
  toolCatalog?: ToolSelectionTool[];
  isLoading: boolean;
}): JSX.Element {
  const orgRoutes = useOrgRoutes();
  const queryClient = useQueryClient();
  const navigate = useNavigate();

  const [tab, setTab] = useState("direct");
  const [page, setPage] = useState(0);
  const [adding, setAdding] = useState<AddTarget>(null);
  const [narrowing, setNarrowing] = useState<ResourceAudienceEntry | null>(
    null,
  );

  const direct = useMemo(() => ownRules(entries), [entries]);
  const inherited = useMemo(() => inheritedRules(entries), [entries]);
  const rows = tab === "direct" ? direct : inherited;

  const visible = pageOf(rows, page);
  const pages = pageCount(rows.length);

  const setAudience = useSetResourceAudienceMutation({
    onSuccess: async () => {
      await invalidateAllResourceAudience(queryClient);
      setAdding(null);
    },
    onError: () => toast.error("Could not save access. Please try again."),
  });

  const save = (next: SetResourceAudienceEntry[], message: string) => {
    setAudience.mutate(
      {
        request: {
          setResourceAudienceForm: {
            resourceKind: "mcp",
            resourceId,
            entries: next,
          },
        },
      },
      { onSuccess: () => toast.success(message) },
    );
  };

  const changeLevel = (entry: ResourceAudienceEntry, level: AudienceLevel) => {
    // Changing an inherited rule writes a direct one for the same principal:
    // this surface only ever owns rules naming the resource, so the
    // organization-wide rule it inherits is left exactly as it was.
    save(
      withLevel(direct, entry.principalUrn, level),
      `${entry.displayName}: ${LEVEL_LABEL[level].toLowerCase()}.`,
    );
  };

  const removePrincipals = (principalUrns: string[]) => {
    const names = principalUrns.length === 1 ? "the rule" : "the rules";
    save(withoutPrincipals(direct, principalUrns), `Removed ${names}.`);
  };

  const changeNarrowing = (
    entry: ResourceAudienceEntry,
    next: { tools: string[]; dispositions: string[] },
  ) => {
    save(
      withNarrowing(direct, entry.principalUrn, {
        tools: next.tools,
        // The annotation values and the stored dispositions are the same
        // strings; the generated union just types them more tightly.
        dispositions:
          next.dispositions as SetResourceAudienceEntry["dispositions"],
      }),
      `${entry.displayName}: ${narrowingLabel(next).toLowerCase()}.`,
    );
    setNarrowing(null);
  };

  // An inherited rule belongs to the role that holds it.
  const editRole = (entry: ResourceAudienceEntry) => {
    const roleId = entry.principalUrn.split(":").pop();
    if (!roleId) return;
    void navigate(`${orgRoutes.access.roles.href()}/${roleId}/edit`);
  };

  const addPrincipals = (principalUrns: string[]) => {
    save(
      withAdded(direct, principalUrns),
      principalUrns.length === 1
        ? "Access granted."
        : `${principalUrns.length} added.`,
    );
  };

  if (isLoading) return <SkeletonTable />;

  return (
    <div>
      <div className="mb-4">
        <Heading variant="h4">Manage access</Heading>
      </div>

      <div className="border-border border">
        <Tabs
          className="gap-0"
          value={tab}
          onValueChange={(next) => {
            setTab(next);
            setPage(0);
          }}
        >
          <div className="border-border bg-muted/30 flex flex-wrap items-center justify-between gap-3 border-b px-4">
            <PageTabsList>
              <PageTabsTrigger value="direct">Direct access</PageTabsTrigger>
              <PageTabsTrigger value="organization">
                Organization access
              </PageTabsTrigger>
            </PageTabsList>

            <RequireScope
              scope="org:admin"
              level="component"
              reason="Only organization admins can change access."
            >
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  {/* The bar behind it is tinted, so the button keeps its own
                      surface rather than dissolving into the header. */}
                  <Button
                    variant="secondary"
                    size="sm"
                    className="bg-background"
                  >
                    <Button.LeftIcon>
                      <Plus className="h-4 w-4" />
                    </Button.LeftIcon>
                    <Button.Text>Grant access</Button.Text>
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent
                  align="end"
                  className="w-[var(--radix-dropdown-menu-trigger-width)]"
                >
                  <DropdownMenuItem onClick={() => setAdding("people")}>
                    A person
                  </DropdownMenuItem>
                  <DropdownMenuItem onClick={() => setAdding("groups")}>
                    A role
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            </RequireScope>
          </div>

          <TabsContent value={tab} forceMount>
            {visible.length === 0 ? (
              <div className="px-4 py-12 text-center">
                <Text muted small>
                  {tab === "direct"
                    ? `Nobody has been given access to ${resourceName ?? "this server"} yet.`
                    : "No organization-wide rules cover this server."}
                </Text>
              </div>
            ) : (
              <div className="divide-border divide-y">
                {visible.map((entry) => (
                  <AccessRow
                    key={`${entry.appliesTo}-${entry.principalUrn}`}
                    entry={entry}
                    onChangeLevel={(level) => changeLevel(entry, level)}
                    onNarrow={() => setNarrowing(entry)}
                    onRemove={() => removePrincipals([entry.principalUrn])}
                    onEditRole={() => editRole(entry)}
                    pending={setAudience.isPending}
                  />
                ))}
              </div>
            )}
          </TabsContent>
        </Tabs>
      </div>

      {pages > 1 && (
        <div className="mt-4 flex items-center justify-center gap-4">
          <Button
            variant="tertiary"
            size="sm"
            disabled={page === 0}
            onClick={() => setPage((p) => Math.max(0, p - 1))}
          >
            <Button.LeftIcon>
              <ChevronLeft className="h-4 w-4" />
            </Button.LeftIcon>
            <Button.Text>Previous</Button.Text>
          </Button>
          <Text muted small>
            {page + 1} of {pages}
          </Text>
          <Button
            variant="tertiary"
            size="sm"
            disabled={page >= pages - 1}
            onClick={() => setPage((p) => Math.min(pages - 1, p + 1))}
          >
            <Button.Text>Next</Button.Text>
            <Button.RightIcon>
              <ChevronRight className="h-4 w-4" />
            </Button.RightIcon>
          </Button>
        </div>
      )}

      {narrowing && (
        <ToolNarrowingDialog
          serverId={resourceId}
          serverName={resourceName}
          catalog={toolCatalog}
          tools={narrowing.tools ?? []}
          dispositions={narrowing.dispositions ?? []}
          pending={setAudience.isPending}
          onSave={(next) => changeNarrowing(narrowing, next)}
          onClose={() => setNarrowing(null)}
        />
      )}

      {adding && (
        <AddAudienceDialog
          title={adding === "people" ? "Add people" : "Add roles"}
          description={
            adding === "people"
              ? `Give people access to ${resourceName ?? "this server"} only.`
              : `Give a role access to ${resourceName ?? "this server"} only.`
          }
          kinds={adding === "people" ? ["user"] : ["role"]}
          alreadyAdded={direct.map((entry) => entry.principalUrn)}
          pending={setAudience.isPending}
          onAdd={addPrincipals}
          onClose={() => setAdding(null)}
        />
      )}
    </div>
  );
}

function AccessRow({
  entry,
  onChangeLevel,
  onNarrow,
  onRemove,
  onEditRole,
  pending,
}: {
  entry: ResourceAudienceEntry;
  onChangeLevel: (level: AudienceLevel) => void;
  onNarrow: () => void;
  onRemove: () => void;
  onEditRole: () => void;
  pending: boolean;
}): JSX.Element {
  const userId =
    entry.kind === "user" ? entry.principalUrn.replace(/^user:/, "") : null;
  const ownRule = entry.appliesTo === "resource";

  // An inherited rule is not this page's to edit: its level and narrowing
  // belong to the role that holds it, and a direct rule alongside it would
  // only widen access — grants add, they never subtract. So the row reads,
  // and sends you to the role when you want to change it.
  if (!ownRule) {
    return (
      <AccessListRow
        title={entry.displayName}
        description={entry.description}
        meta={
          <Text muted small className="shrink-0">
            Can {LEVEL_VERB[entry.level]} on every server
          </Text>
        }
        removeLabel={`Remove ${entry.displayName}`}
      >
        {entry.kind === "role" && (
          <Button
            variant="tertiary"
            size="sm"
            onClick={onEditRole}
            className="h-auto px-1 py-0 font-sans normal-case tracking-normal underline decoration-dotted underline-offset-4 hover:decoration-solid"
          >
            <Button.Text className="font-sans normal-case tracking-normal">
              Edit role
            </Button.Text>
          </Button>
        )}
      </AccessListRow>
    );
  }

  return (
    <AccessListRow
      title={
        userId ? (
          <IdentityLink identifier={{ userId }}>
            {entry.displayName}
          </IdentityLink>
        ) : (
          entry.displayName
        )
      }
      description={entry.description}
      onRemove={onRemove}
      removeLabel={`Remove ${entry.displayName}`}
      removeDisabled={pending}
    >
      <RequireScope
        scope="org:admin"
        level="component"
        reason="Only organization admins can change access."
      >
        <InlineChoice
          lead="Can"
          value={LEVEL_VERB[entry.level]}
          disabled={pending}
          options={[
            ...GRANTABLE_LEVELS.map((level) => ({
              label: LEVEL_MENU_LABEL[level],
              description: LEVEL_DESCRIPTION[level],
              onSelect: () => onChangeLevel(level),
            })),
            {
              label: LEVEL_MENU_LABEL.blocked,
              description: LEVEL_DESCRIPTION.blocked,
              onSelect: () => onChangeLevel("blocked"),
              separatorBefore: true,
            },
          ]}
        />
      </RequireScope>
      {/* Only connect access reaches individual tools; view and manage are
          about the server itself, so there is nothing to narrow. */}
      {entry.level === "use" && (
        <RequireScope
          scope="org:admin"
          level="component"
          reason="Only organization admins can change access."
        >
          <InlineChoice
            lead="to"
            value={narrowingLabel(entry)}
            disabled={pending}
            options={[
              { label: "All tools", onSelect: onNarrow },
              { label: "Specific tools…", onSelect: onNarrow },
            ]}
          />
        </RequireScope>
      )}
    </AccessListRow>
  );
}

export { ACCESS_PAGE_SIZE };
