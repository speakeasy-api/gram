import { FeatureRequestModal } from "@/components/FeatureRequestModal";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/Sheet";
import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import type { KillswitchCapability } from "@gram/client/models/components/killswitchcapability.js";
import type { KillswitchComingSoonCapability } from "@gram/client/models/components/killswitchcomingsooncapability.js";
import type { KillswitchDetail } from "@gram/client/models/components/killswitchdetail.js";
import type { KillswitchMCPServer } from "@gram/client/models/components/killswitchmcpserver.js";
import type { KillswitchMutationReceipt } from "@gram/client/models/components/killswitchmutationreceipt.js";
import type { KillswitchPreviewOverlapsResult } from "@gram/client/models/components/killswitchpreviewoverlapsresult.js";
import { useMemo, useRef, useState } from "react";
import {
  conflictName,
  draftToSchedule,
  draftToScope,
  newOperationId,
  scheduleLabel,
  scopeLabel,
  serverDiff,
  type DraftErrors,
  type EditorDraft,
  unicodeLength,
  validateDraft,
} from "./killswitch-view-model";

type Props = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  mode: "create" | "edit";
  members: AccessMember[];
  servers: KillswitchMCPServer[];
  capabilities: KillswitchCapability[];
  comingSoon: KillswitchComingSoonCapability[];
  capabilitiesLoading?: boolean;
  capabilitiesError?: unknown;
  onRetryCapabilities?: () => void;
  initial?: KillswitchDetail;
  initiallyStale?: boolean;
  onPreview: (draft: EditorDraft) => Promise<KillswitchPreviewOverlapsResult>;
  onSubmit: (
    draft: EditorDraft,
    operationId: string,
    expectedVersion?: number,
  ) => Promise<KillswitchMutationReceipt>;
  onRefreshConflict?: () => Promise<KillswitchDetail>;
  onView?: (id: string) => void;
};

function concurrentChanges(
  baseline: KillswitchDetail,
  latest: KillswitchDetail,
): string[] {
  const changes: string[] = [];
  if (JSON.stringify(baseline.scope) !== JSON.stringify(latest.scope))
    changes.push("MCP server scope");
  if (JSON.stringify(baseline.schedule) !== JSON.stringify(latest.schedule))
    changes.push("schedule");
  if (baseline.externalNote !== latest.externalNote)
    changes.push("public message");
  if (baseline.internalNote !== latest.internalNote)
    changes.push("internal note");
  return changes;
}

function toLocalInput(value: Date): string {
  const local = new Date(value.getTime() - value.getTimezoneOffset() * 60_000);
  return local.toISOString().slice(0, 16);
}

function initialDraft(initial?: KillswitchDetail): EditorDraft {
  if (!initial) {
    return {
      userId: "",
      capabilityKey: "",
      scopeType: "",
      serverIds: [],
      startType: "now",
      startsAt: "",
      endType: "until_lifted",
      endsAt: "",
      externalNote: "",
      internalNote: "",
    };
  }
  return {
    userId: initial.userId,
    capabilityKey: initial.capabilityKey,
    scopeType: initial.scope.type,
    serverIds:
      initial.scope.type === "selected_servers"
        ? [...initial.scope.serverIds]
        : [],
    startType: initial.status === "active" ? "now" : initial.schedule.start,
    startsAt:
      initial.status !== "active" && initial.schedule.start === "scheduled"
        ? toLocalInput(initial.schedule.startsAt)
        : "",
    endType: initial.schedule.end,
    endsAt:
      initial.schedule.end === "bounded"
        ? toLocalInput(initial.schedule.endsAt)
        : "",
    externalNote: initial.externalNote,
    internalNote: initial.internalNote,
  };
}

type EditorContentsProps = Props & {
  isSubmitting: boolean;
  setIsSubmitting: (pending: boolean) => void;
};

export function KillswitchEditorSheet(props: Props): JSX.Element {
  const [isSubmitting, setIsSubmitting] = useState(false);
  const handleOpenChange = (open: boolean) => {
    if (!open && isSubmitting) return;
    props.onOpenChange(open);
  };

  return (
    <Sheet open={props.open} onOpenChange={handleOpenChange}>
      {props.open && (
        <EditorContents
          key={`${props.mode}-${props.initial?.id ?? "new"}`}
          {...props}
          onOpenChange={handleOpenChange}
          isSubmitting={isSubmitting}
          setIsSubmitting={setIsSubmitting}
        />
      )}
    </Sheet>
  );
}

function EditorContents({
  onOpenChange,
  mode,
  members,
  servers,
  capabilities,
  comingSoon,
  capabilitiesLoading,
  capabilitiesError,
  onRetryCapabilities,
  initial,
  initiallyStale,
  onPreview,
  onSubmit,
  onRefreshConflict,
  onView,
  isSubmitting,
  setIsSubmitting,
}: EditorContentsProps): JSX.Element {
  const [draft, setDraft] = useState<EditorDraft>(() => initialDraft(initial));
  const [errors, setErrors] = useState<DraftErrors>({});
  const [operationId, setOperationId] = useState(newOperationId);
  const [expectedVersion, setExpectedVersion] = useState(initial?.version);
  const [preview, setPreview] = useState<KillswitchPreviewOverlapsResult>();
  const [previewFingerprint, setPreviewFingerprint] = useState("");
  const [mutationError, setMutationError] = useState<string>();
  const [stale, setStale] = useState(initiallyStale ?? false);
  const [comparisonBaseline, setComparisonBaseline] = useState(initial);
  const [latestChanges, setLatestChanges] = useState<string[]>();
  const [isReviewing, setIsReviewing] = useState(false);
  const [receipt, setReceipt] = useState<KillswitchMutationReceipt>();
  const [pickerOpen, setPickerOpen] = useState(false);
  const [pickerServerIds, setPickerServerIds] = useState<string[]>([]);
  const [serverSearch, setServerSearch] = useState("");
  const [requestOpen, setRequestOpen] = useState(false);
  const formRef = useRef<HTMLFieldSetElement>(null);
  const submittingRef = useRef(false);
  const draftRef = useRef(draft);
  const draftGeneration = useRef(0);
  const draftFingerprint = useRef(JSON.stringify(draft));
  const previewRequest = useRef(0);
  const conflictRefreshRequest = useRef(0);

  const catalogUnavailable =
    mode === "create" &&
    (Boolean(capabilitiesLoading) || Boolean(capabilitiesError));
  const member = members.find((item) => item.id === draft.userId);
  const serverNames = useMemo(
    () => new Map(servers.map((server) => [server.id, server.name])),
    [servers],
  );
  const diff = comparisonBaseline
    ? serverDiff(
        comparisonBaseline.scope,
        draftToScope(draft),
        servers.map((server) => server.id),
      )
    : null;
  const pickerSelectedServerIds = useMemo(
    () => new Set(pickerServerIds),
    [pickerServerIds],
  );
  const filteredServers = useMemo(() => {
    if (!pickerOpen) return [];
    const search = serverSearch.toLowerCase();
    return servers.filter((server) =>
      `${server.name} ${server.projectId}`.toLowerCase().includes(search),
    );
  }, [pickerOpen, serverSearch, servers]);
  const deletedSelectedIds = draft.serverIds.filter(
    (id) => !serverNames.has(id),
  );

  const update = <K extends keyof EditorDraft>(
    key: K,
    value: EditorDraft[K],
  ) => {
    if (submittingRef.current) return;
    const nextDraft = { ...draftRef.current, [key]: value };
    draftRef.current = nextDraft;
    draftGeneration.current += 1;
    draftFingerprint.current = JSON.stringify(nextDraft);
    previewRequest.current += 1;
    conflictRefreshRequest.current += 1;
    setDraft(nextDraft);
    setErrors((current) => ({ ...current, [key]: undefined }));
    setPreview(undefined);
    setPreviewFingerprint("");
    setIsReviewing(false);
    setMutationError(undefined);
    setOperationId(newOperationId());
  };

  const reviewImpact = async (): Promise<boolean> => {
    if (submittingRef.current) return false;
    const reviewedDraft = draftRef.current;
    const nextErrors = validateDraft(reviewedDraft);
    setErrors(nextErrors);
    if (Object.keys(nextErrors).length > 0) {
      requestAnimationFrame(() => {
        formRef.current
          ?.querySelector<HTMLElement>("[aria-invalid='true']")
          ?.focus();
      });
      return false;
    }
    const request = ++previewRequest.current;
    const generation = draftGeneration.current;
    const reviewedFingerprint = draftFingerprint.current;
    setIsReviewing(true);
    setMutationError(undefined);
    try {
      const result = await onPreview(reviewedDraft);
      if (
        request !== previewRequest.current ||
        generation !== draftGeneration.current ||
        reviewedFingerprint !== draftFingerprint.current
      )
        return false;
      setPreview(result);
      setPreviewFingerprint(reviewedFingerprint);
      return true;
    } catch (error) {
      if (
        request === previewRequest.current &&
        generation === draftGeneration.current &&
        reviewedFingerprint === draftFingerprint.current
      ) {
        setMutationError(
          error instanceof Error ? error.message : "Unable to review impact.",
        );
      }
      return false;
    } finally {
      if (request === previewRequest.current) setIsReviewing(false);
    }
  };

  const submit = async () => {
    if (submittingRef.current) return;
    if (previewFingerprint !== draftFingerprint.current) {
      await reviewImpact();
      return;
    }
    submittingRef.current = true;
    setIsSubmitting(true);
    setMutationError(undefined);
    try {
      const result = await onSubmit(
        draftRef.current,
        operationId,
        expectedVersion,
      );
      setReceipt(result);
    } catch (error) {
      const conflict = conflictName(error);
      if (conflict === "version_conflict") {
        setStale(true);
      } else if (conflict === "operation_conflict") {
        setOperationId(newOperationId());
        setMutationError(
          "This operation ID was already used for a different change. A new ID is ready; review and retry.",
        );
      } else {
        setMutationError(
          error instanceof Error
            ? error.message
            : "Unable to save the killswitch.",
        );
      }
    } finally {
      submittingRef.current = false;
      setIsSubmitting(false);
    }
  };

  const reviewLatest = async () => {
    if (!onRefreshConflict || submittingRef.current) return;
    const request = ++conflictRefreshRequest.current;
    const generation = draftGeneration.current;
    setIsReviewing(true);
    try {
      const latest = await onRefreshConflict();
      if (
        request !== conflictRefreshRequest.current ||
        generation !== draftGeneration.current
      )
        return;
      setLatestChanges(
        comparisonBaseline ? concurrentChanges(comparisonBaseline, latest) : [],
      );
      setComparisonBaseline(latest);
      setExpectedVersion(latest.version);
      setOperationId(newOperationId());
      setStale(false);
      setPreview(undefined);
      setPreviewFingerprint("");
      setMutationError(undefined);
    } catch (error) {
      if (
        request === conflictRefreshRequest.current &&
        generation === draftGeneration.current
      ) {
        setMutationError(
          error instanceof Error
            ? error.message
            : "Unable to load the latest version.",
        );
      }
    } finally {
      if (request === conflictRefreshRequest.current) setIsReviewing(false);
    }
  };

  if (receipt) {
    return (
      <SheetContent className="w-full overflow-y-auto sm:max-w-xl">
        <SheetHeader>
          <SheetTitle>Killswitch saved</SheetTitle>
          <SheetDescription>
            Version {receipt.version} is recorded
            {receipt.replayed ? " from the original operation" : ""}.
          </SheetDescription>
        </SheetHeader>
        <div className="space-y-4 px-4">
          {mode === "create" && (
            <Button
              variant="secondary"
              onClick={() => {
                const retainedUser = draft.userId;
                const nextDraft = { ...initialDraft(), userId: retainedUser };
                draftRef.current = nextDraft;
                draftGeneration.current += 1;
                draftFingerprint.current = JSON.stringify(nextDraft);
                setDraft(nextDraft);
                setReceipt(undefined);
                setOperationId(newOperationId());
                setPreview(undefined);
                setPreviewFingerprint("");
              }}
            >
              Add another killswitch for {member?.name || "this member"}
            </Button>
          )}
          <Button onClick={() => onView?.(receipt.id)}>View killswitch</Button>
        </div>
      </SheetContent>
    );
  }

  return (
    <>
      <SheetContent className="w-full overflow-y-auto sm:max-w-xl">
        <SheetHeader>
          <SheetTitle>
            {mode === "create" ? "New killswitch" : "Edit killswitch"}
          </SheetTitle>
          <SheetDescription>
            One member and one capability create one independently managed
            killswitch.
          </SheetDescription>
        </SheetHeader>

        <fieldset
          ref={formRef}
          className="space-y-7 px-4 pb-4"
          disabled={isSubmitting}
        >
          <fieldset className="space-y-3">
            <legend className="font-medium">Who</legend>
            {mode === "edit" ? (
              <p>{member?.name ?? "Deleted member"}</p>
            ) : (
              <select
                aria-label="Team member"
                aria-invalid={Boolean(errors.userId)}
                aria-describedby={
                  errors.userId ? "killswitch-user-error" : undefined
                }
                className="border-input bg-background h-9 w-full border px-3 text-sm"
                value={draft.userId}
                onChange={(event) => update("userId", event.target.value)}
              >
                <option value="">Choose a team member</option>
                {members.map((item) => (
                  <option key={item.id} value={item.id}>
                    {item.name} — {item.email}
                  </option>
                ))}
              </select>
            )}
            <FieldError id="killswitch-user-error" message={errors.userId} />
          </fieldset>

          <fieldset
            className="space-y-3"
            aria-describedby={
              errors.capabilityKey ? "killswitch-capability-error" : undefined
            }
          >
            <legend className="font-medium">What to turn off</legend>
            {capabilitiesLoading && (
              <p className="text-muted-foreground text-sm">
                Loading capability catalog…
              </p>
            )}
            {Boolean(capabilitiesError) && (
              <Alert variant="error">
                <AlertTitle>Unable to load capability catalog</AlertTitle>
                <AlertDescription>
                  {capabilitiesError instanceof Error
                    ? capabilitiesError.message
                    : "The capability catalog is unavailable."}
                  {onRetryCapabilities && (
                    <div className="mt-2">
                      <Button
                        size="sm"
                        variant="secondary"
                        onClick={onRetryCapabilities}
                      >
                        Try again
                      </Button>
                    </div>
                  )}
                </AlertDescription>
              </Alert>
            )}
            {mode === "edit" ? (
              <p>{initial?.capabilityLabel ?? "MCP tool calls"}</p>
            ) : (
              capabilities.map((capability) => (
                <label
                  key={capability.key}
                  className="flex items-center gap-2 border p-3"
                >
                  <input
                    type="radio"
                    name="capability"
                    aria-invalid={Boolean(errors.capabilityKey)}
                    checked={draft.capabilityKey === capability.key}
                    disabled={catalogUnavailable}
                    onChange={() => update("capabilityKey", capability.key)}
                  />
                  {capability.label}
                </label>
              ))
            )}
            <div className="border border-dashed p-3 text-sm">
              <div className="font-medium">More capabilities — Coming soon</div>
              {comingSoon.length > 0 && (
                <div className="text-muted-foreground">
                  {comingSoon.map((item) => item.label).join(", ")}
                </div>
              )}
              <Button
                variant="tertiary"
                size="sm"
                onClick={() => setRequestOpen(true)}
              >
                Request a capability
              </Button>
            </div>
            <FieldError
              id="killswitch-capability-error"
              message={errors.capabilityKey}
            />
          </fieldset>

          <fieldset
            className="space-y-3"
            aria-describedby={
              errors.scopeType || errors.serverIds
                ? "killswitch-scope-error"
                : undefined
            }
          >
            <legend className="font-medium">Which MCP servers</legend>
            <label className="flex items-start gap-2 border p-3">
              <input
                type="radio"
                name="scope"
                aria-invalid={Boolean(errors.scopeType)}
                checked={draft.scopeType === "all_servers"}
                onChange={() => update("scopeType", "all_servers")}
              />
              <span>
                <strong>All MCP servers</strong>
                <br />
                <span className="text-muted-foreground text-sm">
                  Includes current and future organization servers.
                </span>
              </span>
            </label>
            <label className="flex items-start gap-2 border p-3">
              <input
                type="radio"
                name="scope"
                aria-invalid={Boolean(errors.scopeType || errors.serverIds)}
                checked={draft.scopeType === "selected_servers"}
                onChange={() => update("scopeType", "selected_servers")}
              />
              <span>
                <strong>Selected servers</strong>
                <br />
                <span className="text-muted-foreground text-sm">
                  Choose one or more servers across projects.
                </span>
              </span>
            </label>
            {draft.scopeType === "selected_servers" && (
              <div className="space-y-2">
                <Button
                  variant="secondary"
                  size="sm"
                  onClick={() => {
                    if (submittingRef.current) return;
                    setPickerServerIds(draft.serverIds);
                    setServerSearch("");
                    setPickerOpen(true);
                  }}
                >
                  Choose servers ({draft.serverIds.length})
                </Button>
                {draft.serverIds.length > 0 && (
                  <p className="text-sm">
                    {draft.serverIds
                      .map((id) => serverNames.get(id) ?? "Deleted MCP server")
                      .join(", ")}
                  </p>
                )}
                {deletedSelectedIds.map((id) => (
                  <Button
                    key={id}
                    variant="tertiary"
                    size="sm"
                    onClick={() =>
                      update(
                        "serverIds",
                        draft.serverIds.filter((serverId) => serverId !== id),
                      )
                    }
                  >
                    Remove deleted MCP server
                  </Button>
                ))}
              </div>
            )}
            <FieldError
              id="killswitch-scope-error"
              message={errors.scopeType ?? errors.serverIds}
            />
          </fieldset>

          <fieldset className="space-y-3">
            <legend className="font-medium">Schedule</legend>
            <div className="grid gap-3 sm:grid-cols-2">
              <label className="space-y-1 text-sm">
                Starts
                <select
                  aria-label="Start timing"
                  className="border-input bg-background block h-9 w-full border px-2"
                  value={draft.startType}
                  onChange={(event) =>
                    update(
                      "startType",
                      event.target.value as EditorDraft["startType"],
                    )
                  }
                >
                  <option value="now">Now</option>
                  <option value="scheduled">At a scheduled time</option>
                </select>
              </label>
              <label className="space-y-1 text-sm">
                Ends
                <select
                  aria-label="End timing"
                  className="border-input bg-background block h-9 w-full border px-2"
                  value={draft.endType}
                  onChange={(event) =>
                    update(
                      "endType",
                      event.target.value as EditorDraft["endType"],
                    )
                  }
                >
                  <option value="until_lifted">Until lifted</option>
                  <option value="bounded">At a specific time</option>
                </select>
              </label>
            </div>
            {draft.startType === "scheduled" && (
              <label className="block space-y-1 text-sm">
                Start date and time
                <Input
                  type="datetime-local"
                  aria-invalid={Boolean(errors.startsAt)}
                  aria-describedby={
                    errors.startsAt ? "killswitch-start-error" : undefined
                  }
                  value={draft.startsAt}
                  onChange={(value) => update("startsAt", value)}
                />
                <FieldError
                  id="killswitch-start-error"
                  message={errors.startsAt}
                />
              </label>
            )}
            {draft.endType === "bounded" && (
              <label className="block space-y-1 text-sm">
                End date and time
                <Input
                  type="datetime-local"
                  aria-invalid={Boolean(errors.endsAt)}
                  aria-describedby={
                    errors.endsAt ? "killswitch-end-error" : undefined
                  }
                  value={draft.endsAt}
                  onChange={(value) => update("endsAt", value)}
                />
                <FieldError id="killswitch-end-error" message={errors.endsAt} />
              </label>
            )}
          </fieldset>

          <NoteField
            id="killswitch-external-note"
            label="Public message shown to the member"
            description="Shown exactly as plain text. Do not include confidential internal details."
            value={draft.externalNote}
            max={500}
            error={errors.externalNote}
            onChange={(value) => update("externalNote", value)}
          />
          <NoteField
            id="killswitch-internal-note"
            label="Internal note"
            description="Visible only to organization admins."
            value={draft.internalNote}
            max={4000}
            error={errors.internalNote}
            onChange={(value) => update("internalNote", value)}
          />

          {diff &&
            (diff.removed.length > 0 ||
              comparisonBaseline?.scope.type === "all_servers") && (
              <Alert variant="warning">
                <AlertTitle>
                  Narrowing to selected servers reduces Killswitch coverage
                </AlertTitle>
                <AlertDescription>
                  {comparisonBaseline?.scope.type === "all_servers" && (
                    <p>
                      Future MCP servers will no longer be covered
                      automatically, even when every current server is selected.
                    </p>
                  )}
                  {diff.removed.length > 0 && (
                    <p>
                      {draft.startType === "now"
                        ? "Removed servers regain access immediately."
                        : "Removed servers remain available when this Killswitch starts."}
                    </p>
                  )}
                  <div>Added: {formatIds(diff.added, serverNames)}</div>
                  <div>Unchanged: {formatIds(diff.unchanged, serverNames)}</div>
                  <div>Removed: {formatIds(diff.removed, serverNames)}</div>
                </AlertDescription>
              </Alert>
            )}

          <div className="border bg-muted/30 p-3 text-sm">
            <div className="font-medium">Impact summary</div>
            <p className="text-muted-foreground">
              Overlaps include only Killswitches for this member and capability
              whose server scope and schedule intersect.
            </p>
            <p>
              {draft.scopeType
                ? draft.scopeType === "all_servers"
                  ? "All current and future MCP servers"
                  : `${draft.serverIds.length} selected server${draft.serverIds.length === 1 ? "" : "s"}`
                : "Server scope not chosen"}
            </p>
            <p>
              {scheduleLabel(
                draftToSchedule({
                  ...draft,
                  startsAt: draft.startsAt || new Date().toISOString(),
                  endsAt: draft.endsAt || new Date().toISOString(),
                }),
              )}
            </p>
            {preview && (
              <div>
                <p>
                  {preview.overlaps.length === 0
                    ? "No overlapping killswitches."
                    : `${preview.overlaps.length} overlapping killswitch${preview.overlaps.length === 1 ? "" : "es"} remain relevant.`}
                  {preview.truncated
                    ? " Additional overlaps are not shown."
                    : ""}
                </p>
                {preview.overlaps.length > 0 && (
                  <ul className="mt-2 list-disc space-y-1 pl-5">
                    {preview.overlaps.map((overlap) => (
                      <li key={overlap.id}>
                        {scopeLabel(overlap.scope, serverNames)} —{" "}
                        {scheduleLabel(overlap.schedule)} ({overlap.status})
                      </li>
                    ))}
                  </ul>
                )}
              </div>
            )}
          </div>

          {stale && (
            <Alert variant="warning">
              <AlertTitle>
                This killswitch changed while you were editing
              </AlertTitle>
              <AlertDescription>
                Your draft is preserved. Load the latest version, review the
                impact again, and explicitly resubmit.
                <div className="mt-2">
                  <Button
                    size="sm"
                    variant="secondary"
                    disabled={isReviewing}
                    onClick={() => void reviewLatest()}
                  >
                    Review latest version
                  </Button>
                </div>
              </AlertDescription>
            </Alert>
          )}
          {latestChanges && (
            <Alert variant="warning">
              <AlertTitle>Latest server changes loaded</AlertTitle>
              <AlertDescription>
                {latestChanges.length > 0
                  ? `Changed concurrently: ${latestChanges.join(", ")}.`
                  : "No editable fields changed in the latest version."}{" "}
                Your draft is preserved. Review impact again before
                resubmitting.
              </AlertDescription>
            </Alert>
          )}
          {mutationError && (
            <Alert variant="error">
              <AlertTitle>Unable to continue</AlertTitle>
              <AlertDescription>{mutationError}</AlertDescription>
            </Alert>
          )}
        </fieldset>

        <SheetFooter className="border-t">
          <Button
            variant="secondary"
            disabled={isReviewing || isSubmitting || catalogUnavailable}
            onClick={() => void reviewImpact()}
          >
            {isReviewing ? "Reviewing…" : "Review impact"}
          </Button>
          <Button
            disabled={
              isReviewing || isSubmitting || stale || catalogUnavailable
            }
            onClick={() => void submit()}
          >
            {isSubmitting
              ? "Saving…"
              : mode === "create"
                ? "Turn off MCP tool calls"
                : "Save new version"}
          </Button>
          <Button
            variant="tertiary"
            disabled={isSubmitting}
            onClick={() => onOpenChange(false)}
          >
            Cancel
          </Button>
        </SheetFooter>
      </SheetContent>

      <Dialog
        open={pickerOpen}
        onOpenChange={(open) => {
          if (submittingRef.current) return;
          setPickerOpen(open);
          if (!open) setServerSearch("");
        }}
      >
        {pickerOpen && (
          <Dialog.Content className="max-h-[80vh] overflow-y-auto">
            <fieldset disabled={isSubmitting} className="space-y-4">
              <Dialog.Header>
                <Dialog.Title>Select MCP servers</Dialog.Title>
                <Dialog.Description>
                  Selection is one complete, editable server set.
                </Dialog.Description>
              </Dialog.Header>
              <Input
                aria-label="Search MCP servers"
                placeholder="Search servers or projects"
                value={serverSearch}
                onChange={(value) => {
                  if (!submittingRef.current) setServerSearch(value);
                }}
              />
              <div className="space-y-2">
                {filteredServers.length === 0 ? (
                  <p className="text-muted-foreground text-sm">
                    No MCP servers match this search.
                  </p>
                ) : (
                  filteredServers.map((server) => (
                    <label
                      key={server.id}
                      className="flex items-center gap-2 border p-2"
                    >
                      <input
                        type="checkbox"
                        checked={pickerSelectedServerIds.has(server.id)}
                        onChange={(event) => {
                          if (submittingRef.current) return;
                          setPickerServerIds((current) =>
                            event.target.checked
                              ? [...new Set([...current, server.id])]
                              : current.filter((id) => id !== server.id),
                          );
                        }}
                      />
                      <span>
                        {server.name}
                        <span className="text-muted-foreground ml-2 text-xs">
                          Project {server.projectId}
                        </span>
                      </span>
                    </label>
                  ))
                )}
              </div>
              <Dialog.Footer>
                <Button
                  variant="secondary"
                  onClick={() => {
                    if (!submittingRef.current) setPickerOpen(false);
                  }}
                >
                  Cancel
                </Button>
                <Button
                  onClick={() => {
                    if (submittingRef.current) return;
                    update("serverIds", pickerServerIds);
                    setPickerOpen(false);
                  }}
                >
                  Apply selection
                </Button>
              </Dialog.Footer>
            </fieldset>
          </Dialog.Content>
        )}
      </Dialog>

      <FeatureRequestModal
        isOpen={requestOpen}
        onClose={() => setRequestOpen(false)}
        title="Request a capability"
        description="Tell us which curated capability would be useful. This does not create a killswitch or promise availability."
        actionType="killswitch_capability"
        requestInput={{
          label: "Capability",
          placeholder: "What should a Killswitch be able to turn off?",
          telemetryField: "capability",
        }}
      />
    </>
  );
}

function NoteField({
  id,
  label,
  description,
  value,
  max,
  error,
  onChange,
}: {
  id: string;
  label: string;
  description: string;
  value: string;
  max: number;
  error?: string;
  onChange: (value: string) => void;
}): JSX.Element {
  return (
    <div className="space-y-1">
      <Label htmlFor={id}>{label}</Label>
      <p id={`${id}-description`} className="text-muted-foreground text-xs">
        {description}
      </p>
      <textarea
        id={id}
        className="border-input bg-background min-h-24 w-full border p-2 text-sm"
        value={value}
        onChange={(event) => onChange(event.target.value)}
        aria-invalid={Boolean(error)}
        aria-describedby={`${id}-description${error ? ` ${id}-error` : ""}`}
      />
      <div className="flex justify-between gap-2">
        <FieldError id={`${id}-error`} message={error} />
        <span className="text-muted-foreground ml-auto text-xs">
          {unicodeLength(value)}/{max}
        </span>
      </div>
    </div>
  );
}

function FieldError({
  id,
  message,
}: {
  id?: string;
  message?: string;
}): JSX.Element | null {
  return message ? (
    <p id={id} role="alert" className="text-destructive text-xs">
      {message}
    </p>
  ) : null;
}

function formatIds(ids: string[], names: ReadonlyMap<string, string>): string {
  return ids.length === 0
    ? "None"
    : ids.map((id) => names.get(id) ?? "Deleted MCP server").join(", ");
}
