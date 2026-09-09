import { AccessListRow, InlineChoice } from "@/components/access/AccessListRow";
import { IdentityLink } from "@/components/identity-link";
import { RequireScope } from "@/components/require-scope";
import { useRBAC } from "@/hooks/useRBAC";
import { Button } from "@/components/ui/Button";
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
  pageCount,
  pageOf,
  withAdded,
  withLevel,
  withNarrowing,
  withoutRules,
  ruleId,
} from "./manageAccessState";
import {
  narrowingLabel,
  GRANTABLE_LEVELS,
  LEVEL_DESCRIPTION,
  LEVEL_LABEL,
  LEVEL_MENU_LABEL,
  LEVEL_VERB,
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
  version,
  toolCatalog,
  isLoading,
}: {
  resourceId: string;
  resourceName?: string;
  entries: ResourceAudienceEntry[];
  /** Fingerprint of the rules this list was read from. */
  version: string;
  /** The server's tools, when this backend exposes a catalogue. */
  toolCatalog?: ToolSelectionTool[];
  isLoading: boolean;
}): JSX.Element {
  const orgRoutes = useOrgRoutes();
  const queryClient = useQueryClient();
  const navigate = useNavigate();

  // The row controls are gated by RequireScope; the remove button carries the
  // same gate as a disabled state so it cannot be clicked without the scope.
  const { hasAnyScope } = useRBAC();
  const canManage = hasAnyScope(["org:admin"]);
  const [tab, setTab] = useState("people");
  const [page, setPage] = useState(0);
  const [adding, setAdding] = useState<AddTarget>(null);
  const [narrowing, setNarrowing] = useState<ResourceAudienceEntry | null>(
    null,
  );

  const direct = useMemo(() => ownRules(entries), [entries]);
  // "Everyone" is people too — every member of the organization — and a rule
  // naming them here is this page's to edit, unlike a role's.
  const people = useMemo(
    () =>
      entries.filter(
        (entry) => entry.kind === "user" || entry.kind === "everyone",
      ),
    [entries],
  );
  const roles = useMemo(
    () =>
      entries.filter(
        (entry) => entry.kind !== "user" && entry.kind !== "everyone",
      ),
    [entries],
  );
  // Rules covering every server, which a rule added here cannot narrow.
  const orgWide = useMemo(
    () => entries.filter((entry) => entry.appliesTo !== "resource"),
    [entries],
  );
  const rows = tab === "people" ? people : roles;

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
            // The list this edit was based on. A save against a stale one is
            // refused rather than quietly replacing someone else's work.
            expectedVersion: version,
          },
        },
      },
      {
        onSuccess: () => {
          toast.success(message);
        },
      },
    );
  };

  const changeLevel = (entry: ResourceAudienceEntry, level: AudienceLevel) => {
    // Changing an inherited rule writes a direct one for the same principal:
    // this surface only ever owns rules naming the resource, so the
    // organization-wide rule it inherits is left exactly as it was.
    save(
      withLevel(direct, ruleId(entry), level),
      `${entry.displayName}: ${LEVEL_LABEL[level].toLowerCase()}.`,
    );
  };

  // "All tools" is a reset, so it writes an unnarrowed rule rather than
  // reopening the picker on the selection it is meant to clear.
  const widen = (entry: ResourceAudienceEntry) => {
    save(
      withNarrowing(direct, ruleId(entry), { tools: [], dispositions: [] }),
      `${entry.displayName}: all tools.`,
    );
  };

  const removeRules = (ids: string[]) => {
    const names = ids.length === 1 ? "the rule" : "the rules";
    save(withoutRules(direct, ids), `Removed ${names}.`);
  };

  const changeNarrowing = (
    entry: ResourceAudienceEntry,
    next: { tools: string[]; dispositions: string[] },
  ) => {
    save(
      withNarrowing(direct, ruleId(entry), {
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
    // What you just granted is a person, so show the list it lands in.
    setTab("people");
    setPage(0);
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
              <PageTabsTrigger value="people">People</PageTabsTrigger>
              <PageTabsTrigger value="roles">Roles</PageTabsTrigger>
            </PageTabsList>

            <RequireScope
              scope="org:admin"
              level="component"
              reason="Only organization admins can change access."
            >
              {/* The bar behind it is tinted, so the button keeps its own
                  surface rather than dissolving into the header. */}
              <Button
                variant="secondary"
                size="sm"
                className="bg-background"
                onClick={() => setAdding("people")}
              >
                <Button.LeftIcon>
                  <Plus className="h-4 w-4" />
                </Button.LeftIcon>
                <Button.Text>Grant access</Button.Text>
              </Button>
            </RequireScope>
          </div>

          <TabsContent value={tab} forceMount>
            {visible.length === 0 ? (
              <div className="px-4 py-12 text-center">
                <Text muted small>
                  {tab === "people" ? (
                    <>
                      Nobody has been given access to{" "}
                      {resourceName ?? "this server"} directly.
                    </>
                  ) : (
                    "No role reaches this server."
                  )}
                </Text>
              </div>
            ) : (
              <div className="divide-border divide-y">
                {visible.map((entry) => (
                  <AccessRow
                    key={`${entry.appliesTo}-${ruleId(entry)}`}
                    entry={entry}
                    onChangeLevel={(level) => changeLevel(entry, level)}
                    onNarrow={() => setNarrowing(entry)}
                    onWiden={() => widen(entry)}
                    onRemove={() => removeRules([ruleId(entry)])}
                    onEditRole={() => editRole(entry)}
                    canManage={canManage}
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
          title="Grant access"
          description={`Give people access to ${resourceName ?? "this server"} only. To give a role access, edit the role.`}
          kinds={["user"]}
          alreadyAdded={direct.map((entry) => entry.principalUrn)}
          // A principal an organization-wide rule already covers cannot be
          // narrowed by adding a rule here — grants add, they never subtract
          // — so say what it already has instead of offering a no-op.
          alreadyReaches={orgWide
            // A narrowed organization rule leaves room to grant more here, so
            // only unrestricted ones make a principal unaddable.
            .filter(
              (entry) =>
                (entry.tools ?? []).length === 0 &&
                (entry.dispositions ?? []).length === 0,
            )
            .map((entry) => ({
              principalUrn: entry.principalUrn,
              reason: `Already can ${LEVEL_VERB[entry.level]} on every server`,
            }))}
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
  onWiden,
  onRemove,
  onEditRole,
  canManage,
  pending,
}: {
  entry: ResourceAudienceEntry;
  onChangeLevel: (level: AudienceLevel) => void;
  onNarrow: () => void;
  onWiden: () => void;
  onRemove: () => void;
  onEditRole: () => void;
  canManage: boolean;
  pending: boolean;
}): JSX.Element {
  const userId =
    entry.kind === "user" ? entry.principalUrn.replace(/^user:/, "") : null;
  // Editable here only when the rule names this server and belongs to people
  // — a person, or everyone in the organization. A role is an organization
  // object: its rule belongs to the role editor, whether it covers one server
  // or all of them.
  const ownRule =
    entry.appliesTo === "resource" &&
    (entry.kind === "user" || entry.kind === "everyone");

  // An inherited rule is not this page's to edit: its level and narrowing
  // belong to the role that holds it, and a direct rule alongside it would
  // only widen access — grants add, they never subtract. So the row reads,
  // and sends you to the role when you want to change it.
  if (!ownRule) {
    return (
      <AccessListRow
        title={entry.displayName}
        // Two lines, like a direct row: who it is, then how far it reaches.
        // Who it is, then how far it reaches — which is now the thing the two
        // read-only cases differ by.
        description={[
          entry.description,
          `${
            entry.level === "use"
              ? `Can connect to ${narrowingLabel(entry)}`
              : `Can ${LEVEL_VERB[entry.level]}`
          } on ${
            entry.appliesTo === "resource" ? "this server" : "every server"
          }`,
        ]
          .filter(Boolean)
          .join(" · ")}
        meta={
          entry.kind === "role" ? (
            <Button
              variant="tertiary"
              size="sm"
              onClick={onEditRole}
              className="hover:bg-transparent h-auto shrink-0 self-center px-0 py-0 font-sans normal-case tracking-normal underline decoration-dotted underline-offset-4 hover:decoration-solid"
            >
              <Button.Text className="font-sans normal-case tracking-normal">
                Edit role
              </Button.Text>
            </Button>
          ) : undefined
        }
        removeLabel={`Remove ${entry.displayName}`}
      />
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
      removeDisabled={pending || !canManage}
      removeReason="Only organization admins can change access."
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
          // Blocking is off the menu: the server writes and enforces one, and
          // a row already holding a block says so in its own value, so
          // offering it again is a no-op write that can fail on the version
          // check or the lockout guardrail.
          options={GRANTABLE_LEVELS.map((level) => ({
            label: LEVEL_MENU_LABEL[level],
            description: LEVEL_DESCRIPTION[level],
            onSelect: () => onChangeLevel(level),
          }))}
        />
      </RequireScope>
      {/* Connect reaches individual tools, and a block is the only rule that
          takes one away, so both can be narrowed. View and manage are about
          the server itself and have nothing to narrow. */}
      {(entry.level === "use" || entry.level === "blocked") && (
        <RequireScope
          scope="org:admin"
          level="component"
          reason="Only organization admins can change access."
        >
          <InlineChoice
            lead="to"
            value={
              entry.level === "blocked" && narrowingLabel(entry) === "all tools"
                ? "any tool"
                : narrowingLabel(entry)
            }
            disabled={pending}
            options={[
              {
                label: entry.level === "blocked" ? "Any tool" : "All tools",
                onSelect: onWiden,
              },
              { label: "Specific tools…", onSelect: onNarrow },
            ]}
          />
        </RequireScope>
      )}
    </AccessListRow>
  );
}
