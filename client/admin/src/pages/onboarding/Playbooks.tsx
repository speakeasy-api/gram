import {
  keepPreviousData,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { Link, useSearch } from "@tanstack/react-router";
import { ChevronsUpDownIcon, MoreHorizontalIcon, StarIcon } from "lucide-react";
import { useCallback, useRef, useState, type JSX } from "react";
import type { AdminOnboardingPlaybook } from "@gram/admin-client/models/components/adminonboardingplaybook";
import type { AdminOnboardingStep } from "@gram/admin-client/models/components/adminonboardingstep";
import type { AdminOnboardingUseCase } from "@gram/admin-client/models/components/adminonboardingusecase";

import { useConfirmDialog } from "@/components/ConfirmDialog";
import { useOnUnmount } from "@/hooks/useOnUnmount";
import { PlaybookStepEditor } from "@/components/onboarding/PlaybookStepEditor";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Command,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { organizationQuery, organizationsListQuery } from "@/lib/adminQueries";
import { cn } from "@/lib/utils";
import { errorMessage } from "@/lib/gramAdminApi";
import {
  assignAdminOrganizationOnboardingPlaybook,
  createAdminOnboardingPlaybook,
  createAdminOnboardingUseCase,
  deleteAdminOnboardingPlaybook,
  deleteAdminOnboardingUseCase,
  onboardingPlaybooksQuery,
  onboardingStepsQuery,
  onboardingUseCasesQuery,
  organizationOnboardingPlaybookQuery,
  updateAdminOnboardingPlaybook,
} from "@/lib/gramAdminClient";

/** A use case being created. */
interface UseCaseDraft {
  name: string;
  slug: string;
  description: string;
}

/** Who a playbook is for: a use case (shared) or one customer, never both. */
type Owner = "use-case" | "customer";

/**
 * A playbook being created or edited; id is absent for a new one. A customer's
 * playbook is assigned to them on save; a use case's may be its default.
 */
interface PlaybookDraft {
  id?: string;
  owner: Owner;
  /** The owner was chosen before the dialog opened, so it is not asked. */
  ownerLocked: boolean;
  useCaseId?: string;
  organizationId?: string;
  organizationName?: string;
  name: string;
  description: string;
  isDefault: boolean;
  stepSlugs: string[];
}

/** How long the assigned playbook's row stays highlighted. */
const HIGHLIGHT_MS = 1500;

function slugify(value: string): string {
  return value
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
}

// The file route renders through this, so the page itself stays a plain
// component a test can hand an organization to.
export function OnboardingPlaybooksRoute(): JSX.Element {
  const { organization } = useSearch({ from: "/onboarding-playbooks" });
  return <OnboardingPlaybooks organizationId={organization} />;
}

/**
 * Use cases and playbooks. Nothing seeds them: staff define the outcomes and
 * compose playbooks from the code-defined steps. A playbook belongs to a use
 * case, shared and possibly its default, or to one customer, in which case it
 * is assigned to them. Scoped to a customer (from their Overview page), the
 * Playbooks table lists their own beside the use cases', a new playbook is
 * theirs, any playbook can be assigned to them, and the one assigned is
 * highlighted for a moment so the eye lands on it.
 */
export function OnboardingPlaybooks({
  organizationId,
}: {
  organizationId?: string;
}): JSX.Element {
  const queryClient = useQueryClient();
  const scoped = organizationId !== undefined;
  const useCasesQuery = onboardingUseCasesQuery();
  const playbooksQuery = onboardingPlaybooksQuery(organizationId);
  const assignmentQuery = organizationOnboardingPlaybookQuery(
    organizationId ?? "",
  );
  const useCases = useQuery({ ...useCasesQuery, throwOnError: false });
  const playbooks = useQuery({ ...playbooksQuery, throwOnError: false });
  const steps = useQuery({ ...onboardingStepsQuery(), throwOnError: false });
  const organization = useQuery({
    ...organizationQuery(organizationId ?? ""),
    enabled: scoped,
    throwOnError: false,
  });
  const assignment = useQuery({
    ...assignmentQuery,
    enabled: scoped,
    throwOnError: false,
  });
  const [confirm, confirmDialog] = useConfirmDialog();
  const [useCaseDraft, setUseCaseDraft] = useState<UseCaseDraft | null>(null);
  const [playbookDraft, setPlaybookDraft] = useState<PlaybookDraft | null>(
    null,
  );
  // A dialog's failure shows in the dialog; a row action's shows under the tables.
  const [dialogError, setDialogError] = useState("");
  const [actionError, setActionError] = useState("");
  // The assigned playbook whose highlight has faded. An assignment clears
  // it, so the newly assigned row is highlighted until its own fade, even
  // when it is the one that faded before.
  const [fadedId, setFadedId] = useState<string | undefined>(undefined);
  const fadeTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  useOnUnmount(() => {
    if (fadeTimer.current) clearTimeout(fadeTimer.current);
  });
  // Callback ref for the highlighted row: the fade starts as it attaches and
  // is dropped if the row goes before it ends.
  const fade = useCallback((row: HTMLTableRowElement | null) => {
    if (fadeTimer.current) clearTimeout(fadeTimer.current);
    fadeTimer.current = null;
    if (!row) return;
    const id = row.dataset.playbook;
    fadeTimer.current = setTimeout(() => setFadedId(id), HIGHLIGHT_MS);
  }, []);
  const refresh = async () => {
    await queryClient.invalidateQueries({ queryKey: useCasesQuery.queryKey });
    await queryClient.invalidateQueries({ queryKey: playbooksQuery.queryKey });
    if (scoped) {
      await queryClient.invalidateQueries({
        queryKey: assignmentQuery.queryKey,
      });
    }
  };
  const failDialog = (err: unknown) => setDialogError(errorMessage(err));
  const failAction = (err: unknown) => setActionError(errorMessage(err));
  const createUseCase = useMutation({
    mutationFn: createAdminOnboardingUseCase,
    onSuccess: async () => {
      setUseCaseDraft(null);
      await refresh();
    },
    onError: failDialog,
  });
  const removeUseCase = useMutation({
    mutationFn: deleteAdminOnboardingUseCase,
    onSuccess: refresh,
    onError: failAction,
  });
  const createPlaybook = useMutation({
    mutationFn: async (draft: PlaybookDraft) => {
      const customer = draft.owner === "customer";
      const created = await createAdminOnboardingPlaybook({
        useCaseId: customer ? undefined : draft.useCaseId,
        organizationId: customer ? draft.organizationId : undefined,
        name: draft.name,
        description: draft.description,
        isDefault: customer ? false : draft.isDefault,
        stepSlugs: draft.stepSlugs,
      });
      if (customer && draft.organizationId) {
        try {
          await assignAdminOrganizationOnboardingPlaybook({
            organizationId: draft.organizationId,
            playbookId: created.id,
          });
        } catch (err) {
          // The stack refused it. Leave nothing behind; the reason is what
          // matters, so a failed clean-up must not replace it.
          await deleteAdminOnboardingPlaybook({
            playbookId: created.id,
          }).catch(() => undefined);
          throw err;
        }
      }
      return created;
    },
    onSuccess: async () => {
      setPlaybookDraft(null);
      await refresh();
    },
    onError: failDialog,
  });
  const savePlaybook = useMutation({
    mutationFn: updateAdminOnboardingPlaybook,
    onSuccess: async () => {
      setPlaybookDraft(null);
      await refresh();
    },
    onError: failDialog,
  });
  const makeDefault = useMutation({
    mutationFn: updateAdminOnboardingPlaybook,
    onSuccess: refresh,
    onError: failAction,
  });
  const assign = useMutation({
    mutationFn: assignAdminOrganizationOnboardingPlaybook,
    onSuccess: async () => {
      await refresh();
      setFadedId(undefined);
    },
    onError: failAction,
  });
  const removePlaybook = useMutation({
    mutationFn: deleteAdminOnboardingPlaybook,
    onSuccess: refresh,
    onError: failAction,
  });

  const scopePending =
    scoped && (organization.isPending || assignment.isPending);
  if (
    useCases.isPending ||
    playbooks.isPending ||
    steps.isPending ||
    scopePending
  ) {
    return <p role="status">Loading use cases…</p>;
  }
  const scopeFailed = scoped && (!organization.data || !assignment.data);
  if (!useCases.data || !playbooks.data || !steps.data || scopeFailed) {
    return (
      <div className="space-y-3">
        <h1 className="text-2xl font-semibold">Use Cases &amp; Playbooks</h1>
        <p role="alert">
          {errorMessage(
            useCases.error ??
              playbooks.error ??
              steps.error ??
              organization.error ??
              assignment.error,
          )}
        </p>
        <Button
          onClick={() => {
            void useCases.refetch();
            void playbooks.refetch();
            void steps.refetch();
            if (scoped) {
              void organization.refetch();
              void assignment.refetch();
            }
          }}
        >
          Retry
        </Button>
      </div>
    );
  }

  const organizationName = scoped ? organization.data?.name : undefined;
  const assignedId = scoped ? assignment.data?.playbook?.id : undefined;
  const busy =
    createUseCase.isPending ||
    removeUseCase.isPending ||
    createPlaybook.isPending ||
    savePlaybook.isPending ||
    makeDefault.isPending ||
    assign.isPending ||
    removePlaybook.isPending;
  const allPlaybooks = playbooks.data.playbooks;
  const playbooksOf = (useCase: AdminOnboardingUseCase) =>
    allPlaybooks.filter((playbook) => playbook.useCaseId === useCase.id);
  const nameOfUseCase = (id: string | undefined) =>
    useCases.data.useCases.find((useCase) => useCase.id === id)?.name ?? "";
  const openUseCase = () => {
    setDialogError("");
    setUseCaseDraft({ name: "", slug: "", description: "" });
  };
  const openPlaybook = (draft: PlaybookDraft) => {
    setDialogError("");
    setPlaybookDraft(draft);
  };
  // From a use case's row the playbook is that use case's; from the table it
  // is whoever the form says, starting with the scoped customer if any.
  const newPlaybook = (useCase?: AdminOnboardingUseCase) =>
    openPlaybook({
      owner: useCase || !scoped ? "use-case" : "customer",
      ownerLocked: useCase !== undefined,
      useCaseId: useCase?.id,
      organizationId: useCase ? undefined : organizationId,
      organizationName: useCase ? undefined : organizationName,
      name: "",
      description: "",
      // The first playbook of a use case is offered as its default.
      isDefault: useCase !== undefined && playbooksOf(useCase).length === 0,
      stepSlugs: [],
    });
  const editPlaybook = (playbook: AdminOnboardingPlaybook) =>
    openPlaybook({
      id: playbook.id,
      owner: playbook.organizationId ? "customer" : "use-case",
      ownerLocked: true,
      useCaseId: playbook.useCaseId,
      organizationId: playbook.organizationId,
      organizationName: playbook.organizationName,
      name: playbook.name,
      description: playbook.description,
      isDefault: playbook.isDefault,
      stepSlugs: playbook.steps.map((step) => step.slug),
    });
  const submitPlaybook = () => {
    if (!playbookDraft) return;
    if (playbookDraft.id) {
      savePlaybook.mutate({
        playbookId: playbookDraft.id,
        name: playbookDraft.name,
        description: playbookDraft.description,
        isDefault: playbookDraft.isDefault,
        stepSlugs: playbookDraft.stepSlugs,
      });
    } else {
      createPlaybook.mutate(playbookDraft);
    }
  };
  const promote = (playbook: AdminOnboardingPlaybook) => {
    setActionError("");
    makeDefault.mutate({
      playbookId: playbook.id,
      name: playbook.name,
      description: playbook.description,
      isDefault: true,
      stepSlugs: playbook.steps.map((step) => step.slug),
    });
  };
  const assignTo = (playbook: AdminOnboardingPlaybook) => {
    if (!organizationId) return;
    setActionError("");
    assign.mutate({ organizationId, playbookId: playbook.id });
  };
  const deleteUseCase = async (useCase: AdminOnboardingUseCase) => {
    const confirmed = await confirm({
      title: `Delete ${useCase.name}?`,
      description:
        "Its playbooks go with it. An organization on one of them falls back to its setup task selection.",
      confirmLabel: "Delete",
      destructive: true,
    });
    if (!confirmed) return;
    setActionError("");
    removeUseCase.mutate({ useCaseId: useCase.id });
  };
  const deletePlaybook = async (playbook: AdminOnboardingPlaybook) => {
    const confirmed = await confirm({
      title: `Delete ${playbook.name}?`,
      description:
        "An organization on this playbook falls back to its setup task selection.",
      confirmLabel: "Delete",
      destructive: true,
    });
    if (!confirmed) return;
    setActionError("");
    removePlaybook.mutate({ playbookId: playbook.id });
  };

  return (
    <div className="space-y-8">
      <div className="space-y-3">
        <div className="flex flex-wrap items-start justify-between gap-4">
          <div className="space-y-1">
            <h1 className="text-2xl font-semibold">
              Use Cases &amp; Playbooks
            </h1>
            <p className="text-muted-foreground max-w-3xl text-sm">
              A playbook is ordered steps for a use case or for one customer.
              Each use case's default is the one the survey assigns.
            </p>
          </div>
          <Button disabled={busy} onClick={openUseCase}>
            Create use case
          </Button>
        </div>
        {organizationName ? (
          <p className="text-muted-foreground text-sm">
            Scoped to {organizationName}: its own playbooks show with the use
            cases', and a new playbook is its.{" "}
            <Link
              to="/onboarding-playbooks"
              className="underline underline-offset-4 hover:no-underline"
            >
              Show all
            </Link>
          </p>
        ) : null}
      </div>

      <section className="space-y-3">
        <h2 className="text-base font-semibold">Use cases</h2>
        {useCases.data.useCases.length === 0 ? (
          <p className="text-muted-foreground text-sm">No use cases yet.</p>
        ) : (
          <Table aria-label="Use cases">
            <TableHeader>
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead>Description</TableHead>
                <TableHead>Playbook</TableHead>
                <TableHead className="w-12">
                  <span className="sr-only">Actions</span>
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {useCases.data.useCases.map((useCase) => {
                const fallback = playbooksOf(useCase).find(
                  (playbook) => playbook.isDefault,
                );
                return (
                  <TableRow key={useCase.id} data-use-case={useCase.id}>
                    <TableCell className="align-top font-medium whitespace-normal">
                      {useCase.name}
                    </TableCell>
                    <TableCell className="text-muted-foreground max-w-xl align-top whitespace-normal">
                      {useCase.description}
                    </TableCell>
                    <TableCell className="align-top whitespace-normal">
                      {fallback ? (
                        fallback.name
                      ) : (
                        <span className="text-muted-foreground">
                          No default playbook
                        </span>
                      )}
                    </TableCell>
                    <TableCell className="align-top">
                      <RowMenu
                        label={`Actions for ${useCase.name}`}
                        disabled={busy}
                      >
                        <DropdownMenuItem onSelect={() => newPlaybook(useCase)}>
                          New playbook
                        </DropdownMenuItem>
                        <DropdownMenuItem
                          variant="destructive"
                          onSelect={() => void deleteUseCase(useCase)}
                        >
                          Delete use case
                        </DropdownMenuItem>
                      </RowMenu>
                    </TableCell>
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        )}
      </section>

      <section className="space-y-3">
        <div className="flex flex-wrap items-center justify-between gap-4">
          <h2 className="text-base font-semibold">Playbooks</h2>
          <Button
            variant="outline"
            size="sm"
            disabled={busy}
            onClick={() => newPlaybook()}
          >
            New playbook
          </Button>
        </div>
        {allPlaybooks.length === 0 ? (
          <p className="text-muted-foreground text-sm">No playbooks yet.</p>
        ) : (
          <Table aria-label="Playbooks">
            <TableHeader>
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead>Applies to</TableHead>
                <TableHead>Steps</TableHead>
                <TableHead className="w-12">
                  <span className="sr-only">Actions</span>
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {allPlaybooks.map((playbook) => {
                const assigned = playbook.id === assignedId;
                const highlighted = assigned && assignedId !== fadedId;
                return (
                  <TableRow
                    key={playbook.id}
                    ref={highlighted ? fade : undefined}
                    data-playbook={playbook.id}
                    data-highlighted={highlighted || undefined}
                    className={cn(highlighted && "bg-accent")}
                  >
                    <TableCell className="align-top font-medium whitespace-normal">
                      <PlaybookName playbook={playbook} />
                    </TableCell>
                    <TableCell className="align-top whitespace-normal">
                      {appliesTo(playbook)}
                    </TableCell>
                    <TableCell className="text-muted-foreground max-w-xl align-top whitespace-normal">
                      {stepChain(playbook)}
                    </TableCell>
                    <TableCell className="align-top">
                      <PlaybookMenu
                        playbook={playbook}
                        assigned={assigned}
                        organizationName={organizationName}
                        busy={busy}
                        onEdit={() => editPlaybook(playbook)}
                        onMakeDefault={() => promote(playbook)}
                        onAssign={() => assignTo(playbook)}
                        onDelete={() => void deletePlaybook(playbook)}
                      />
                    </TableCell>
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        )}
      </section>
      {actionError ? (
        <p role="alert" className="text-destructive text-sm">
          {actionError}
        </p>
      ) : null}

      <Dialog
        open={useCaseDraft !== null}
        onOpenChange={(open) => {
          if (!open && !busy) setUseCaseDraft(null);
        }}
      >
        <DialogContent>
          {useCaseDraft ? (
            <form
              className="space-y-4"
              aria-label="New use case"
              onSubmit={(event) => {
                event.preventDefault();
                createUseCase.mutate({
                  slug: useCaseDraft.slug,
                  name: useCaseDraft.name,
                  description: useCaseDraft.description,
                });
              }}
            >
              <DialogHeader>
                <DialogTitle>Create use case</DialogTitle>
                <DialogDescription>
                  An outcome a customer can pick in the survey. The slug is what
                  the survey sends.
                </DialogDescription>
              </DialogHeader>
              <label className="block space-y-1 text-sm">
                <span className="font-medium">Name</span>
                <Input
                  aria-label="Use case name"
                  value={useCaseDraft.name}
                  disabled={busy}
                  onChange={(event) =>
                    setUseCaseDraft({
                      ...useCaseDraft,
                      name: event.target.value,
                      slug: slugify(event.target.value),
                    })
                  }
                />
              </label>
              <label className="block space-y-1 text-sm">
                <span className="font-medium">Slug</span>
                {/* Derived from the name; shown so staff see what the survey sends. */}
                <Input
                  aria-label="Use case slug"
                  value={useCaseDraft.slug}
                  disabled
                  readOnly
                />
              </label>
              <label className="block space-y-1 text-sm">
                <span className="font-medium">Description</span>
                <Textarea
                  aria-label="Use case description"
                  value={useCaseDraft.description}
                  disabled={busy}
                  onChange={(event) =>
                    setUseCaseDraft({
                      ...useCaseDraft,
                      description: event.target.value,
                    })
                  }
                />
              </label>
              {dialogError ? (
                <p role="alert" className="text-destructive text-sm">
                  {dialogError}
                </p>
              ) : null}
              <DialogFooter>
                <Button
                  type="button"
                  variant="ghost"
                  disabled={busy}
                  onClick={() => setUseCaseDraft(null)}
                >
                  Cancel
                </Button>
                <Button
                  type="submit"
                  disabled={
                    busy || !useCaseDraft.slug || !useCaseDraft.name.trim()
                  }
                >
                  Create use case
                </Button>
              </DialogFooter>
            </form>
          ) : null}
        </DialogContent>
      </Dialog>

      <Dialog
        open={playbookDraft !== null}
        onOpenChange={(open) => {
          if (!open && !busy) setPlaybookDraft(null);
        }}
      >
        <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-xl">
          {playbookDraft ? (
            <PlaybookForm
              draft={playbookDraft}
              useCases={useCases.data.useCases}
              useCaseName={nameOfUseCase(playbookDraft.useCaseId)}
              steps={steps.data.steps}
              error={dialogError}
              busy={busy}
              onChange={setPlaybookDraft}
              onSubmit={submitPlaybook}
              onCancel={() => setPlaybookDraft(null)}
            />
          ) : null}
        </DialogContent>
      </Dialog>
      {confirmDialog}
    </div>
  );
}

function stepChain(playbook: AdminOnboardingPlaybook): string {
  return playbook.steps.map((step) => step.title).join(" → ");
}

/** Who the playbook is for: its use case, or the one customer it is for. */
function appliesTo(playbook: AdminOnboardingPlaybook): string {
  return playbook.useCaseName ?? playbook.organizationName ?? "";
}

/** The trailing "…" menu on a row. */
function RowMenu({
  label,
  disabled,
  children,
}: {
  label: string;
  disabled: boolean;
  children: React.ReactNode;
}): JSX.Element {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          variant="ghost"
          size="icon"
          className="size-8"
          aria-label={label}
          disabled={disabled}
        >
          <MoreHorizontalIcon />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">{children}</DropdownMenuContent>
    </DropdownMenu>
  );
}

/** A small icon that explains itself on hover and to assistive tech. */
function Marker({
  icon: Icon,
  label,
  tip,
}: {
  icon: typeof StarIcon;
  label: string;
  tip: string;
}): JSX.Element {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span
          role="img"
          aria-label={label}
          className="text-muted-foreground inline-flex shrink-0"
        >
          <Icon aria-hidden="true" className="size-3.5 fill-current" />
        </span>
      </TooltipTrigger>
      <TooltipContent>{tip}</TooltipContent>
    </Tooltip>
  );
}

/** The name, starred when it is its use case's default. */
function PlaybookName({
  playbook,
}: {
  playbook: AdminOnboardingPlaybook;
}): JSX.Element {
  return (
    <span className="flex items-center gap-1.5">
      {playbook.isDefault ? (
        <Marker
          icon={StarIcon}
          label="Default playbook"
          tip="Default for this use case"
        />
      ) : (
        // Holds the star's place so names line up.
        <span aria-hidden="true" className="size-3.5 shrink-0" />
      )}
      {playbook.name}
    </span>
  );
}

function PlaybookMenu({
  playbook,
  assigned,
  organizationName,
  busy,
  onEdit,
  onMakeDefault,
  onAssign,
  onDelete,
}: {
  playbook: AdminOnboardingPlaybook;
  assigned: boolean;
  organizationName: string | undefined;
  busy: boolean;
  onEdit: () => void;
  onMakeDefault: () => void;
  onAssign: () => void;
  onDelete: () => void;
}): JSX.Element {
  const shared = playbook.useCaseId !== undefined;
  return (
    <RowMenu label={`Actions for ${playbook.name}`} disabled={busy}>
      <DropdownMenuItem onSelect={onEdit}>Edit</DropdownMenuItem>
      {shared && !playbook.isDefault ? (
        <DropdownMenuItem onSelect={onMakeDefault}>
          Make default
        </DropdownMenuItem>
      ) : null}
      {organizationName && !assigned ? (
        <DropdownMenuItem onSelect={onAssign}>
          Assign to {organizationName}
        </DropdownMenuItem>
      ) : null}
      <DropdownMenuItem variant="destructive" onSelect={onDelete}>
        Delete
      </DropdownMenuItem>
    </RowMenu>
  );
}

function PlaybookForm({
  draft,
  useCases,
  useCaseName,
  steps,
  error,
  busy,
  onChange,
  onSubmit,
  onCancel,
}: {
  draft: PlaybookDraft;
  useCases: AdminOnboardingUseCase[];
  useCaseName: string;
  steps: AdminOnboardingStep[];
  error: string;
  busy: boolean;
  onChange: (next: PlaybookDraft) => void;
  onSubmit: () => void;
  onCancel: () => void;
}): JSX.Element {
  const editing = draft.id !== undefined;
  const customer = draft.owner === "customer";
  const ownerNamed = customer ? draft.organizationName : useCaseName;
  const complete =
    draft.name.trim() !== "" &&
    draft.stepSlugs.length > 0 &&
    (customer ? draft.organizationId !== undefined : !!draft.useCaseId);
  return (
    <form
      className="space-y-4"
      aria-label={editing ? "Edit playbook" : "New playbook"}
      onSubmit={(event) => {
        event.preventDefault();
        onSubmit();
      }}
    >
      <DialogHeader>
        <DialogTitle>{editing ? "Edit playbook" : "New playbook"}</DialogTitle>
        <DialogDescription>
          {draft.ownerLocked
            ? `For ${ownerNamed}.`
            : "For a use case, shared, or for one customer."}
        </DialogDescription>
      </DialogHeader>
      {draft.ownerLocked ? null : (
        <div className="space-y-2">
          <Tabs
            value={draft.owner}
            onValueChange={(owner) =>
              onChange({
                ...draft,
                owner: owner as Owner,
                // Only a use case's playbook can be a default.
                isDefault: owner === "customer" ? false : draft.isDefault,
              })
            }
          >
            <TabsList aria-label="For">
              <TabsTrigger value="use-case" disabled={busy}>
                Use case
              </TabsTrigger>
              <TabsTrigger value="customer" disabled={busy}>
                Customer
              </TabsTrigger>
            </TabsList>
          </Tabs>
          {customer ? (
            <OrganizationPicker
              value={
                draft.organizationId && draft.organizationName
                  ? { id: draft.organizationId, name: draft.organizationName }
                  : undefined
              }
              disabled={busy}
              onChange={(organization) =>
                onChange({
                  ...draft,
                  organizationId: organization?.id,
                  organizationName: organization?.name,
                })
              }
            />
          ) : (
            <Select
              value={draft.useCaseId ?? ""}
              onValueChange={(useCaseId) => onChange({ ...draft, useCaseId })}
              disabled={busy}
            >
              <SelectTrigger aria-label="Use case" className="w-64">
                <SelectValue placeholder="Choose a use case" />
              </SelectTrigger>
              <SelectContent>
                {useCases.map((useCase) => (
                  <SelectItem key={useCase.id} value={useCase.id}>
                    {useCase.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          )}
        </div>
      )}
      <Input
        aria-label="Playbook name"
        placeholder="Playbook name"
        value={draft.name}
        disabled={busy}
        onChange={(event) => onChange({ ...draft, name: event.target.value })}
      />
      <Textarea
        aria-label="Playbook description"
        placeholder="What this playbook gets the customer to"
        value={draft.description}
        disabled={busy}
        onChange={(event) =>
          onChange({ ...draft, description: event.target.value })
        }
      />
      {customer ? null : (
        <label className="flex items-center gap-2 text-sm">
          <Checkbox
            aria-label="Default for this use case"
            checked={draft.isDefault}
            disabled={busy}
            onCheckedChange={(checked) =>
              onChange({ ...draft, isDefault: checked === true })
            }
          />
          Default for this use case
        </label>
      )}
      <PlaybookStepEditor
        steps={steps}
        value={draft.stepSlugs}
        disabled={busy}
        onChange={(stepSlugs) => onChange({ ...draft, stepSlugs })}
      />
      {error ? (
        <p role="alert" className="text-destructive text-sm">
          {error}
        </p>
      ) : null}
      <DialogFooter>
        <Button
          type="button"
          variant="ghost"
          disabled={busy}
          onClick={onCancel}
        >
          Cancel
        </Button>
        <Button type="submit" disabled={busy || !complete}>
          {editing ? "Save playbook" : "Create playbook"}
        </Button>
      </DialogFooter>
    </form>
  );
}

/** The customer a playbook is for, found by name. */
function OrganizationPicker({
  value,
  disabled,
  onChange,
}: {
  value: { id: string; name: string } | undefined;
  disabled: boolean;
  onChange: (next: { id: string; name: string } | undefined) => void;
}): JSX.Element {
  const [open, setOpen] = useState(false);
  const [term, setTerm] = useState("");
  const results = useQuery({
    ...organizationsListQuery({ q: term, disabled_status: "all", limit: 8 }),
    enabled: open && term.length > 0,
    placeholderData: keepPreviousData,
    throwOnError: false,
  });
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          type="button"
          variant="outline"
          role="combobox"
          aria-expanded={open}
          aria-label="Customer"
          disabled={disabled}
          className="w-64 justify-between font-normal"
        >
          {value ? (
            value.name
          ) : (
            <span className="text-muted-foreground">Choose a customer</span>
          )}
          <ChevronsUpDownIcon aria-hidden="true" className="opacity-50" />
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-64 p-0" align="start">
        <Command shouldFilter={false}>
          <CommandInput
            placeholder="Search customers"
            value={term}
            onValueChange={setTerm}
          />
          <CommandList>
            {(results.data?.organizations ?? []).map((organization) => (
              <CommandItem
                key={organization.id}
                value={organization.id}
                onSelect={() => {
                  onChange({ id: organization.id, name: organization.name });
                  setOpen(false);
                }}
              >
                {organization.name}
              </CommandItem>
            ))}
            {term && results.data && results.data.organizations.length === 0 ? (
              <p className="text-muted-foreground px-2 py-1.5">
                No customer matches.
              </p>
            ) : term ? null : (
              <p className="text-muted-foreground px-2 py-1.5">
                Type to search.
              </p>
            )}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}
