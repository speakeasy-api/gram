import { useState, type JSX } from "react";
import { getRouteApi } from "@tanstack/react-router";
import {
  keepPreviousData,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";

import type { AdminPluginRepositoryMigration } from "@gram/admin-client/models/components/adminpluginrepositorymigration";
import { useConfirmDialog } from "@/components/ConfirmDialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Textarea } from "@/components/ui/textarea";
import { statusCode } from "@/lib/adminAuth";
import {
  listPluginRepositoryMigrations,
  pluginRepositoryMigrationInventoryKey as inventoryKey,
} from "@/lib/adminQueries";
import { errorMessage } from "@/lib/gramAdminApi";
import {
  applyPluginRepositoryMigrations,
  reprovisionPluginRepository,
} from "@/lib/gramAdminClient";
import {
  MAX_REPOSITORY_EVIDENCE_LENGTH,
  MAX_REPOSITORY_MIGRATION_ENTRIES,
  MAX_REPOSITORY_REASON_LENGTH,
  parseRepositoryEvidence,
  repositoryEvidence,
  verifiedRepositoryEvidence,
  type RepositoryEvidence,
} from "@/lib/pluginRepositoryEvidence";

const route = getRouteApi("/plugin-repository-migrations");

export function PluginRepositoryMigrationsRoute(): JSX.Element {
  const { after, project } = route.useSearch();
  return (
    <PluginRepositoryMigrationsPage
      key={`${after ?? ""}:${project ?? ""}`}
      initialAfter={after}
      focusedProjectId={project}
    />
  );
}

function focusInventoryRow(row: HTMLTableRowElement | null) {
  row?.focus();
}

export function PluginRepositoryMigrationsPage({
  initialAfter,
  focusedProjectId,
}: {
  initialAfter?: string | undefined;
  focusedProjectId?: string | undefined;
}): JSX.Element {
  const [writeDenied, setWriteDenied] = useState(false);
  const [pages, setPages] = useState<(string | undefined)[]>([initialAfter]);
  const [selectedProject, setSelectedProject] =
    useState<AdminPluginRepositoryMigration>();
  const inventory = useQuery({
    queryKey: [...inventoryKey, pages.at(-1)],
    queryFn: ({ signal }) =>
      listPluginRepositoryMigrations(pages.at(-1), signal),
    placeholderData: keepPreviousData,
  });

  const denied = [401, 403].includes(statusCode(inventory.error) ?? 0);

  function handleWriteError(error: unknown) {
    if (statusCode(error) === 403) setWriteDenied(true);
  }

  return (
    <div className="mx-auto w-full max-w-6xl space-y-8 py-2 lg:py-4">
      <header className="space-y-2">
        <h1 className="text-2xl font-semibold tracking-tight">
          Plugin repository migrations
        </h1>
        <p className="text-muted-foreground max-w-3xl text-sm">
          Bind legacy plugins to their original GitHub repository IDs using
          trusted historical records. A repository at the old owner/name today
          may be a different repository: never use the current slug as evidence
          of ownership. Only platform admins can apply bindings or provision
          replacements.
        </p>
      </header>

      {inventory.isPending && (
        <p role="status">Loading repository inventory…</p>
      )}
      {inventory.isError && (
        <p role="alert" className="text-destructive text-sm">
          {denied
            ? "You do not have access to plugin repository migrations."
            : errorMessage(inventory.error)}
        </p>
      )}
      {!inventory.data && inventory.isError && !denied && (
        <div className="flex gap-2">
          {pages[0] && (
            <Button variant="outline" onClick={() => setPages([undefined])}>
              First page
            </Button>
          )}
          {pages.length > 1 && (
            <Button
              variant="outline"
              onClick={() => setPages((previous) => previous.slice(0, -1))}
            >
              Previous page
            </Button>
          )}
          <Button variant="outline" onClick={() => void inventory.refetch()}>
            Retry inventory
          </Button>
        </div>
      )}
      {writeDenied && (
        <p role="status" className="text-muted-foreground text-sm">
          Platform admin access is required to apply bindings or provision
          replacements. You can still preview evidence.
        </p>
      )}
      {inventory.data && !denied && (
        <>
          <BatchEvidence
            writeDenied={writeDenied}
            onWriteError={handleWriteError}
          />
          {selectedProject && (
            <ResolveRepository
              key={selectedProject.projectId}
              project={selectedProject}
              writeDenied={writeDenied}
              onWriteError={handleWriteError}
              bound={inventory.data.items.some(
                (item) =>
                  item.projectId === selectedProject.projectId && item.bound,
              )}
              onClose={() => setSelectedProject(undefined)}
            />
          )}
          <section aria-labelledby="migration-inventory" className="space-y-3">
            <h2 id="migration-inventory" className="text-lg font-semibold">
              Legacy repository inventory
            </h2>
            <div className="rounded-md border">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Project ID</TableHead>
                    <TableHead>Organization</TableHead>
                    <TableHead>Recorded repository</TableHead>
                    <TableHead>Binding</TableHead>
                    <TableHead>
                      <span className="sr-only">Actions</span>
                    </TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {inventory.data.items.map((item) => (
                    <TableRow
                      key={item.projectId}
                      tabIndex={
                        item.projectId === focusedProjectId ? -1 : undefined
                      }
                      ref={
                        item.projectId === focusedProjectId
                          ? focusInventoryRow
                          : undefined
                      }
                      data-state={
                        item.projectId === focusedProjectId
                          ? "selected"
                          : undefined
                      }
                    >
                      <TableCell className="font-mono text-xs">
                        {item.projectId}
                      </TableCell>
                      <TableCell className="font-mono text-xs">
                        {item.organizationId}
                      </TableCell>
                      <TableCell>
                        {item.repoOwner}/{item.repoName}
                      </TableCell>
                      <TableCell>
                        <Badge variant={item.bound ? "secondary" : "outline"}>
                          {item.bound ? "Bound" : "Unresolved"}
                        </Badge>
                      </TableCell>
                      <TableCell className="text-right">
                        {!item.bound && (
                          <Button
                            variant="outline"
                            size="sm"
                            aria-label={`Resolve project ${item.projectId}`}
                            onClick={() => setSelectedProject(item)}
                          >
                            Resolve
                          </Button>
                        )}
                      </TableCell>
                    </TableRow>
                  ))}
                  {inventory.data.items.length === 0 && (
                    <TableRow>
                      <TableCell colSpan={5} className="text-muted-foreground">
                        No repositories on this page.
                      </TableCell>
                    </TableRow>
                  )}
                </TableBody>
              </Table>
            </div>
            <div className="flex items-center justify-end gap-2">
              {pages[0] ? (
                <Button
                  variant="outline"
                  size="sm"
                  disabled={inventory.isPlaceholderData}
                  onClick={() => setPages([undefined])}
                >
                  First page
                </Button>
              ) : (
                <span className="text-muted-foreground mr-2 text-sm">
                  Page {pages.length}
                </span>
              )}
              <Button
                variant="outline"
                size="sm"
                disabled={pages.length === 1 || inventory.isPlaceholderData}
                onClick={() => setPages((previous) => previous.slice(0, -1))}
              >
                Previous
              </Button>
              <Button
                variant="outline"
                size="sm"
                disabled={
                  !inventory.data.nextCursor || inventory.isPlaceholderData
                }
                onClick={() => {
                  const cursor = inventory.data.nextCursor;
                  if (cursor) setPages((previous) => [...previous, cursor]);
                }}
              >
                Next
              </Button>
            </div>
          </section>
        </>
      )}
    </div>
  );
}

function useEvidencePreview(onWriteError: (error: unknown) => void) {
  const client = useQueryClient();
  const [validationError, setValidationError] = useState<string>();
  const [reviewed, setReviewed] = useState<RepositoryEvidence[]>([]);
  const [attempted, setAttempted] = useState<RepositoryEvidence[]>([]);
  const preview = useMutation({
    mutationFn: (entries: RepositoryEvidence[]) =>
      applyPluginRepositoryMigrations(entries, true),
    onSuccess: (results, entries) =>
      setReviewed(verifiedRepositoryEvidence(entries, results)),
  });
  const apply = useMutation({
    mutationFn: (entries: RepositoryEvidence[]) =>
      applyPluginRepositoryMigrations(entries, false),
    onError: onWriteError,
    onSuccess: () => client.invalidateQueries({ queryKey: inventoryKey }),
  });

  function reset() {
    setValidationError(undefined);
    setReviewed([]);
    setAttempted([]);
    preview.reset();
    apply.reset();
  }

  return {
    reviewed,
    busy: preview.isPending || apply.isPending,
    reset,
    verify(getEntries: () => RepositoryEvidence[]) {
      reset();
      try {
        const entries = getEntries();
        setAttempted(entries);
        preview.mutate(entries);
      } catch (error) {
        setValidationError(errorMessage(error));
      }
    },
    commit: () => {
      if (!reviewed.length || preview.isPending || apply.isPending) return;
      setReviewed([]);
      apply.mutate(reviewed);
    },
    results: apply.data ?? preview.data,
    attempted,
    applied: apply.data !== undefined,
    error:
      validationError ??
      (apply.isError
        ? errorMessage(apply.error)
        : preview.isError
          ? errorMessage(preview.error)
          : undefined),
  };
}

function EvidenceOutcome({
  review,
}: {
  review: ReturnType<typeof useEvidencePreview>;
}) {
  if (review.busy)
    return (
      <p role="status" className="text-muted-foreground text-sm">
        Checking repository evidence…
      </p>
    );
  if (review.error)
    return (
      <p role="alert" className="text-destructive text-sm">
        {review.error}
      </p>
    );
  if (!review.results) return null;

  return (
    <div className="space-y-2" aria-live="polite">
      <h3 className="text-sm font-semibold">
        {review.applied ? "Apply results" : "Preview results"}
      </h3>
      <div className="rounded-md border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Project ID</TableHead>
              <TableHead>Repository ID</TableHead>
              <TableHead>Result</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {review.results.map((result, index) => (
              <TableRow key={index}>
                <TableCell className="font-mono text-xs">
                  {result.projectId}
                </TableCell>
                <TableCell className="font-mono text-xs">
                  {review.attempted.find(
                    (entry) => entry.projectId === result.projectId,
                  )?.repositoryId ?? "—"}
                </TableCell>
                <TableCell className="whitespace-normal">
                  <Badge
                    variant={
                      result.status === "verified" || result.status === "bound"
                        ? "secondary"
                        : "outline"
                    }
                  >
                    {result.status.replaceAll("_", " ")}
                  </Badge>
                  {result.message && (
                    <span className="text-muted-foreground ml-2 break-words text-xs">
                      {result.message}
                    </span>
                  )}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
      {!review.applied && review.reviewed.length === 0 && !review.busy && (
        <p className="text-muted-foreground text-sm">
          No verified entries to apply.
        </p>
      )}
    </div>
  );
}

function BatchEvidence({
  writeDenied,
  onWriteError,
}: {
  writeDenied: boolean;
  onWriteError: (error: unknown) => void;
}) {
  const [source, setSource] = useState("");
  const review = useEvidencePreview(onWriteError);

  return (
    <section
      aria-labelledby="batch-evidence"
      className="space-y-3 rounded-md border p-4 sm:p-5"
    >
      <h2 id="batch-evidence" className="text-lg font-semibold">
        Batch evidence import
      </h2>
      <p id="batch-evidence-help" className="text-muted-foreground text-sm">
        Paste up to {MAX_REPOSITORY_MIGRATION_ENTRIES} JSON records with{" "}
        <code>project_id</code>,<code> repository_id</code> (a decimal string),
        and <code>evidence</code> (a trusted historical reference linking the
        project to that ID). Preview before applying.
      </p>
      <Textarea
        aria-label="Evidence JSON"
        aria-describedby="batch-evidence-help"
        className="min-h-32 font-mono text-xs"
        spellCheck={false}
        value={source}
        disabled={review.busy}
        placeholder={
          '[{"project_id":"<project UUID>","repository_id":"123456789","evidence":"<provisioning record reference>"}]'
        }
        onChange={(event) => {
          review.reset();
          setSource(event.target.value);
        }}
      />
      <EvidenceOutcome review={review} />
      <div className="flex flex-wrap gap-2">
        <Button
          variant="outline"
          disabled={!source.trim() || review.busy}
          onClick={() => review.verify(() => parseRepositoryEvidence(source))}
        >
          Preview evidence
        </Button>
        <Button
          disabled={!review.reviewed.length || review.busy || writeDenied}
          onClick={review.commit}
        >
          {review.reviewed.length
            ? `Apply verified entries (${review.reviewed.length})`
            : "Apply verified entries"}
        </Button>
      </div>
    </section>
  );
}

function focusResolutionHeading(node: HTMLHeadingElement | null) {
  node?.focus();
}

function ResolveRepository({
  project,
  bound,
  writeDenied,
  onWriteError,
  onClose,
}: {
  project: AdminPluginRepositoryMigration;
  bound: boolean;
  writeDenied: boolean;
  onWriteError: (error: unknown) => void;
  onClose: () => void;
}) {
  const [repositoryId, setRepositoryId] = useState("");
  const [evidence, setEvidence] = useState("");
  const [reason, setReason] = useState("");
  const [reasonError, setReasonError] = useState<string>();
  const [waitingForConfirmation, setWaitingForConfirmation] = useState(false);
  const [confirm, confirmation] = useConfirmDialog();
  const review = useEvidencePreview(onWriteError);
  const client = useQueryClient();
  const replacement = useMutation({
    mutationFn: (replacementReason: string) =>
      reprovisionPluginRepository(project.projectId, replacementReason),
    onError: onWriteError,
    onSuccess: () => client.invalidateQueries({ queryKey: inventoryKey }),
  });
  const busy = review.busy || replacement.isPending || waitingForConfirmation;
  const bindingComplete =
    bound ||
    replacement.data?.status === "publish_needed" ||
    review.results?.some(
      (row) => row.status === "bound" || row.status === "already_bound",
    );

  async function requestReplacement() {
    const replacementReason = reason.trim();
    if (writeDenied || !replacementReason) return;
    if (
      new TextEncoder().encode(replacementReason).length >
      MAX_REPOSITORY_REASON_LENGTH
    ) {
      setReasonError(
        `Enter a recovery reason of at most ${MAX_REPOSITORY_REASON_LENGTH} bytes.`,
      );
      return;
    }
    setWaitingForConfirmation(true);
    const accepted = await confirm({
      title: "Provision a replacement repository?",
      description:
        "This creates a new repository for this exact project. It does not recover or transfer the legacy repository. The marketplace install URL stays the same, but you must publish plugins again.",
      details: (
        <div className="space-y-2 text-xs">
          <p className="break-all font-mono">Project: {project.projectId}</p>
          <p className="break-all font-mono">
            Organization: {project.organizationId}
          </p>
          <p className="wrap-anywhere">Reason: {replacementReason}</p>
        </div>
      ),
      confirmLabel: "Provision replacement",
      destructive: true,
    });
    setWaitingForConfirmation(false);
    if (accepted) {
      review.reset();
      replacement.mutate(replacementReason);
    }
  }

  return (
    <section
      aria-labelledby="resolve-repository"
      className="space-y-4 rounded-md border p-4 sm:p-5"
    >
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div className="space-y-1">
          <h2
            id="resolve-repository"
            tabIndex={-1}
            ref={focusResolutionHeading}
            className="text-lg font-semibold"
          >
            {bindingComplete
              ? "Project repository resolution"
              : "Resolve one project"}
          </h2>
          <p className="break-all font-mono text-xs">
            Project: {project.projectId}
          </p>
          <p className="text-muted-foreground break-all text-xs">
            Organization: {project.organizationId} · Recorded repository:{" "}
            {project.repoOwner}/{project.repoName}
          </p>
        </div>
        <Button variant="ghost" size="sm" onClick={onClose} disabled={busy}>
          Close
        </Button>
      </div>
      {!bindingComplete && (
        <div className="grid gap-5 sm:grid-cols-2">
          <div className="space-y-3">
            <h3 className="font-medium">Verify historical evidence</h3>
            <label className="block space-y-1 text-sm">
              <span>Historical GitHub repository ID</span>
              <Input
                value={repositoryId}
                inputMode="numeric"
                disabled={busy}
                onChange={(event) => {
                  review.reset();
                  setRepositoryId(event.target.value);
                }}
              />
            </label>
            <label className="block space-y-1 text-sm">
              <span>Trusted historical reference</span>
              <Input
                value={evidence}
                maxLength={MAX_REPOSITORY_EVIDENCE_LENGTH}
                disabled={busy}
                onChange={(event) => {
                  review.reset();
                  setEvidence(event.target.value);
                }}
              />
            </label>
            <div className="flex flex-wrap gap-2">
              <Button
                variant="outline"
                disabled={busy || !repositoryId.trim() || !evidence.trim()}
                onClick={() => {
                  replacement.reset();
                  review.verify(() => [
                    repositoryEvidence(
                      project.projectId,
                      repositoryId.trim(),
                      evidence,
                    ),
                  ]);
                }}
              >
                Preview project binding
              </Button>
              <Button
                disabled={!review.reviewed.length || busy || writeDenied}
                onClick={review.commit}
              >
                Apply verified binding
              </Button>
            </div>
          </div>
          <div className="space-y-3 sm:border-l sm:pl-5">
            <h3 className="font-medium">Replace the repository</h3>
            <p className="text-muted-foreground text-sm">
              If you cannot prove the original repository’s identity, provision
              a new one for this project.
            </p>
            <label className="block space-y-1 text-sm">
              <span>Replacement reason</span>
              <Textarea
                value={reason}
                maxLength={MAX_REPOSITORY_REASON_LENGTH}
                disabled={busy || writeDenied}
                onChange={(event) => {
                  setReasonError(undefined);
                  setReason(event.target.value);
                }}
              />
            </label>
            <Button
              variant="outline"
              disabled={busy || writeDenied || !reason.trim()}
              onClick={() => void requestReplacement()}
            >
              Review replacement
            </Button>
            {reasonError && (
              <p role="alert" className="text-destructive text-sm">
                {reasonError}
              </p>
            )}
          </div>
        </div>
      )}
      <EvidenceOutcome review={review} />
      {replacement.isError && (
        <p role="alert" className="text-destructive text-sm">
          {errorMessage(replacement.error)}
        </p>
      )}
      {replacement.data && (
        <p role="status" className="text-sm">
          <Badge variant="outline">
            {replacement.data.status.replaceAll("_", " ")}
          </Badge>{" "}
          {replacement.data.message}
        </p>
      )}
      {confirmation}
    </section>
  );
}
