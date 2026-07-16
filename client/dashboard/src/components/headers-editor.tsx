import { RequireScope } from "@/components/require-scope";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Type } from "@/components/ui/type";
import { Alert, Button, Stack } from "@speakeasy-api/moonshine";
import { Eye, EyeOff, Loader2, Plus, Trash2 } from "lucide-react";
import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { toast } from "sonner";

const REDACTED_SECRET = "***";

// EditableHeader is the transport-agnostic shape the editor works with. The
// remote and tunneled server header SDK types are structurally identical, so
// each adapter maps its own type onto this before handing it to the editor.
export type EditableHeader = {
  id: string;
  name: string;
  isRequired: boolean;
  isSecret: boolean;
  value?: string | null;
  valueFromRequestHeader?: string | null;
};

// HeaderWriteFields is the set of mutable fields a create/update sends. Exactly
// one of value / valueFromRequestHeader is populated; a preserved secret sends
// neither (so the backend keeps the stored value).
export type HeaderWriteFields = {
  name: string;
  isRequired: boolean;
  isSecret?: boolean;
  value?: string;
  valueFromRequestHeader?: string;
};

// SuggestedHeader is a header the caller proposes seeding into a pristine,
// empty form (e.g. remote MCP suggests headers from the endpoint's catalog
// entry). It carries only metadata — the user fills in values before saving.
export type SuggestedHeader = {
  name: string;
  isRequired: boolean;
  isSecret: boolean;
};

// HeadersEditorAdapter wires the transport-specific data hooks and mutations
// into the shared editor. Each caller (remote MCP, tunneled MCP) builds one
// from its own generated SDK hooks.
export type HeadersEditorAdapter = {
  headers: EditableHeader[] | undefined;
  isLoading: boolean;
  isSaving: boolean;
  mutationError: Error | null;
  createHeader: (fields: HeaderWriteFields) => Promise<void>;
  updateHeader: (id: string, fields: HeaderWriteFields) => Promise<void>;
  deleteHeader: (id: string) => Promise<void>;
  // refetch returns the canonical header list after a save (or null when the
  // refresh itself failed, so the editor can keep the current drafts).
  refetch: () => Promise<EditableHeader[] | null>;
  invalidate: () => Promise<void>;
};

type HeaderSource = "static" | "request";

type HeaderDraft = {
  key: string;
  /** Set for headers that already exist on the server. */
  id?: string;
  name: string;
  source: HeaderSource;
  staticValue: string;
  valueFromRequestHeader: string;
  isRequired: boolean;
  isSecret: boolean;
  hadSecret: boolean;
  /** Row was seeded from a caller suggestion (and is unsaved). */
  fromCatalog?: boolean;
};

function headerSourceFromServer(header: EditableHeader): HeaderSource {
  if (header.valueFromRequestHeader) {
    return "request";
  }
  return "static";
}

function headerDraftFromServer(header: EditableHeader): HeaderDraft {
  const source = headerSourceFromServer(header);
  const isRedactedSecret = header.isSecret && header.value === REDACTED_SECRET;

  return {
    key: header.id,
    id: header.id,
    name: header.name,
    source,
    staticValue:
      source === "static"
        ? isRedactedSecret
          ? REDACTED_SECRET
          : (header.value ?? "")
        : "",
    valueFromRequestHeader: header.valueFromRequestHeader ?? "",
    isRequired: header.isRequired,
    isSecret: header.isSecret,
    hadSecret: header.isSecret,
  };
}

// A saved secret shows its redacted placeholder (`***`) in the value field. As
// long as the user leaves that placeholder untouched, we keep the existing
// secret rather than overwriting it with the literal redaction string.
function isUnchangedSecret(draft: HeaderDraft): boolean {
  return (
    draft.isSecret && draft.hadSecret && draft.staticValue === REDACTED_SECRET
  );
}

function draftsEqual(a: HeaderDraft[], b: HeaderDraft[]): boolean {
  if (a.length !== b.length) return false;
  for (let index = 0; index < a.length; index += 1) {
    const draft = a[index];
    const other = b[index];
    if (!draft || !other) return false;
    if (
      draft.id !== other.id ||
      draft.name !== other.name ||
      draft.source !== other.source ||
      draft.staticValue !== other.staticValue ||
      draft.valueFromRequestHeader !== other.valueFromRequestHeader ||
      draft.isRequired !== other.isRequired ||
      draft.isSecret !== other.isSecret
    ) {
      return false;
    }
  }
  return true;
}

function validateDrafts(drafts: HeaderDraft[]): string | null {
  const names = new Set<string>();
  for (const draft of drafts) {
    const name = draft.name.trim();
    if (!name) {
      return "Every header needs a name.";
    }
    const normalized = name.toLowerCase();
    if (names.has(normalized)) {
      return `Duplicate header name "${name}".`;
    }
    names.add(normalized);

    if (draft.source === "request" && !draft.valueFromRequestHeader.trim()) {
      return `Header "${name}" needs an inbound request header name.`;
    }

    if (
      draft.source === "static" &&
      !isUnchangedSecret(draft) &&
      draft.staticValue.trim() === ""
    ) {
      return `Header "${name}" needs a static value.`;
    }
  }

  return null;
}

function newHeaderDraft(): HeaderDraft {
  return {
    key: crypto.randomUUID(),
    name: "",
    source: "static",
    staticValue: "",
    valueFromRequestHeader: "",
    isRequired: false,
    isSecret: true,
    hadSecret: false,
  };
}

function headerDraftFromSuggestion(suggestion: SuggestedHeader): HeaderDraft {
  return {
    ...newHeaderDraft(),
    fromCatalog: true,
    name: suggestion.name,
    isRequired: suggestion.isRequired,
    isSecret: suggestion.isSecret,
  };
}

function headerDraftToWriteFields(draft: HeaderDraft): HeaderWriteFields {
  const base = {
    name: draft.name.trim(),
    isRequired: draft.isRequired,
  };

  if (draft.source === "request") {
    return {
      ...base,
      isSecret: false,
      valueFromRequestHeader: draft.valueFromRequestHeader.trim(),
    };
  }

  if (isUnchangedSecret(draft)) {
    return {
      ...base,
      isSecret: true,
    };
  }

  return {
    ...base,
    isSecret: draft.isSecret,
    value: draft.staticValue,
  };
}

export function HeadersEditor({
  adapter,
  title,
  description,
  readOnly = false,
  loading = false,
  aboveContent,
  suggestedHeaders,
}: {
  adapter: HeadersEditorAdapter;
  title: string;
  description: string;
  readOnly?: boolean;
  // Extra loading gate the caller controls (e.g. resolving shared siblings).
  loading?: boolean;
  // Rendered above the header rows — used by callers for context alerts.
  aboveContent?: ReactNode;
  // Headers the caller proposes seeding into a pristine, empty form. Seeded
  // once as unsaved drafts; nothing persists until the user fills values and
  // saves. Callers gate their own fetch so this is empty when not applicable.
  suggestedHeaders?: SuggestedHeader[];
}): JSX.Element {
  const headers = adapter.headers;

  const initialDrafts = useMemo(
    () => (headers ?? []).map(headerDraftFromServer),
    [headers],
  );
  const [drafts, setDrafts] = useState(initialDrafts);
  // The server snapshot the current drafts were last synced from. Used to tell
  // "user hasn't touched anything" apart from "user has unsaved edits" so a
  // background refetch (window refocus, concurrent change) can't silently
  // discard in-progress edits.
  const syncedRef = useRef(initialDrafts);

  useEffect(() => {
    const previousSynced = syncedRef.current;
    syncedRef.current = initialDrafts;
    setDrafts((current) =>
      draftsEqual(current, previousSynced) ? initialDrafts : current,
    );
  }, [initialDrafts]);

  // Caller-suggested headers seed a pristine, empty form so the user only has
  // to fill in values. Suggestions are unsaved drafts — nothing persists until
  // Save. Only seed once, and never when headers already exist or editing is
  // disabled.
  const suggestedDrafts = useMemo(
    () => (suggestedHeaders ?? []).map(headerDraftFromSuggestion),
    [suggestedHeaders],
  );
  const headersEmpty = headers !== undefined && headers.length === 0;
  const [suggestionsSeeded, setSuggestionsSeeded] = useState(false);
  useEffect(() => {
    if (readOnly || loading || suggestionsSeeded) return;
    if (!headersEmpty || suggestedDrafts.length === 0) return;
    setSuggestionsSeeded(true);
    // Only seed a pristine form — never clobber rows the user already added.
    setDrafts((current) => (current.length === 0 ? suggestedDrafts : current));
  }, [readOnly, loading, suggestionsSeeded, headersEmpty, suggestedDrafts]);

  // Freshly seeded suggestions are intentionally value-less; don't greet the
  // user with a "needs a static value" error before they've touched anything.
  const pristineSuggestions =
    suggestionsSeeded && draftsEqual(drafts, suggestedDrafts);

  const validationError = validateDrafts(drafts);
  const dirty = !draftsEqual(drafts, initialDrafts);
  const saveDisabled =
    readOnly ||
    !dirty ||
    adapter.isSaving ||
    validationError !== null ||
    adapter.isLoading;

  const handleSave = async () => {
    // Defensive: the save/add controls are hidden in read-only mode, but a
    // read-only surface must never be mutated.
    if (readOnly || validationError) return;

    const initialById = new Map(
      initialDrafts
        .filter((draft): draft is HeaderDraft & { id: string } => !!draft.id)
        .map((draft) => [draft.id, draft]),
    );
    const keptIds = new Set(
      drafts.flatMap((draft) => (draft.id ? [draft.id] : [])),
    );

    try {
      for (const draft of initialDrafts) {
        if (draft.id && !keptIds.has(draft.id)) {
          await adapter.deleteHeader(draft.id);
        }
      }

      for (const draft of drafts) {
        const fields = headerDraftToWriteFields(draft);
        if (!draft.id) {
          await adapter.createHeader(fields);
          continue;
        }

        const initial = initialById.get(draft.id);
        if (!initial || draftsEqual([draft], [initial])) {
          continue;
        }

        await adapter.updateHeader(draft.id, fields);
      }

      await adapter.invalidate();
      // Intentionally adopt the canonical server state after a save so drafts
      // pick up server-assigned ids and secret redaction. The sync effect
      // preserves unsaved edits, so this reset must be explicit.
      const refreshed = await adapter.refetch();
      if (!refreshed) {
        // Save succeeded, but the refresh failed. Don't treat the missing
        // result as an empty header list — that would wipe the form. Leave the
        // current drafts/cache intact and surface the refresh failure instead.
        toast.success("Upstream headers updated");
        toast.warning("Couldn't refresh headers. Reload to see the latest.");
        return;
      }
      const synced = refreshed.map(headerDraftFromServer);
      syncedRef.current = synced;
      setDrafts(synced);
      toast.success("Upstream headers updated");
    } catch (error) {
      const message =
        error instanceof Error ? error.message : "Failed to update headers";
      toast.error(message);
      await adapter.invalidate();
    }
  };

  return (
    <div className="rounded-lg border p-6">
      <Type variant="subheading" className="mb-1">
        {title}
      </Type>
      <Type muted small className="mb-4">
        {description}
      </Type>
      <Stack gap={4}>
        {aboveContent}

        {adapter.isLoading || loading ? (
          <Type muted small>
            Loading headers…
          </Type>
        ) : drafts.length === 0 ? (
          <Type muted small>
            No upstream headers configured yet.
          </Type>
        ) : (
          <Stack gap={4}>
            {drafts.some((draft) => draft.fromCatalog) && (
              <Type muted small>
                These headers are suggested from this endpoint's MCP catalog
                entry. Fill in the values and save, or remove the ones you don't
                need.
              </Type>
            )}
            {drafts.map((draft, index) => (
              <HeaderDraftRow
                key={draft.key}
                draft={draft}
                readOnly={readOnly}
                onChange={(next) =>
                  setDrafts((current) =>
                    current.map((row, rowIndex) =>
                      rowIndex === index ? next : row,
                    ),
                  )
                }
                onRemove={() =>
                  setDrafts((current) =>
                    current.filter((_, rowIndex) => rowIndex !== index),
                  )
                }
              />
            ))}
          </Stack>
        )}

        {readOnly || loading ? null : (
          <>
            {validationError && dirty && !pristineSuggestions ? (
              <Type small className="text-destructive">
                {validationError}
              </Type>
            ) : null}

            {adapter.mutationError ? (
              <Alert variant="error" dismissible={false}>
                {adapter.mutationError.message}
              </Alert>
            ) : null}

            <RequireScope scope="mcp:write" level="component">
              <Button
                variant="secondary"
                size="md"
                disabled={adapter.isLoading}
                onClick={() =>
                  setDrafts((current) => [...current, newHeaderDraft()])
                }
              >
                <Button.LeftIcon>
                  <Plus className="size-4" />
                </Button.LeftIcon>
                <Button.Text>Add header</Button.Text>
              </Button>
            </RequireScope>

            <Type muted small>
              Static secrets are redacted after save. Leave the redacted value
              unchanged to keep the current secret, or replace it to set a new
              one.
            </Type>

            <Stack direction="horizontal" gap={2}>
              <RequireScope scope="mcp:write" level="component">
                <Button
                  variant="primary"
                  size="md"
                  disabled={saveDisabled}
                  onClick={() => void handleSave()}
                >
                  {adapter.isSaving ? (
                    <>
                      <Button.LeftIcon>
                        <Loader2 className="size-4 animate-spin" />
                      </Button.LeftIcon>
                      <Button.Text>Saving</Button.Text>
                    </>
                  ) : (
                    <Button.Text>Save</Button.Text>
                  )}
                </Button>
              </RequireScope>
            </Stack>
          </>
        )}
      </Stack>
    </div>
  );
}

function HeaderDraftRow({
  draft,
  readOnly,
  onChange,
  onRemove,
}: {
  draft: HeaderDraft;
  readOnly: boolean;
  onChange: (draft: HeaderDraft) => void;
  onRemove: () => void;
}): JSX.Element {
  const [revealed, setRevealed] = useState(false);
  // A saved secret is shown as its redacted placeholder — there's nothing to
  // reveal, so hide the toggle until the user replaces it with a new value.
  const isSavedSecret = draft.staticValue === REDACTED_SECRET;
  const showRevealToggle = draft.isSecret && !isSavedSecret;

  return (
    <div className="rounded-md border p-4">
      <Stack gap={3}>
        <Stack direction="horizontal" gap={3} align="start">
          <div className="min-w-0 flex-1">
            <Type small muted className="mb-1">
              Header name
            </Type>
            <Input
              value={draft.name}
              disabled={readOnly}
              onChange={(value) => onChange({ ...draft, name: value })}
              placeholder="Authorization"
            />
          </div>
          <div className="w-52 shrink-0">
            <Type small muted className="mb-1">
              Value source
            </Type>
            <Select
              value={draft.source}
              disabled={readOnly}
              onValueChange={(value) => {
                const source = value as HeaderSource;
                onChange({
                  ...draft,
                  source,
                  isSecret: source === "static" ? draft.isSecret : false,
                });
              }}
            >
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="static">Static value</SelectItem>
                <SelectItem value="request">From request header</SelectItem>
              </SelectContent>
            </Select>
          </div>
          {readOnly ? null : (
            <RequireScope scope="mcp:write" level="component">
              <Button
                variant="tertiary"
                size="md"
                className="mt-6 shrink-0"
                onClick={onRemove}
                aria-label={`Remove header ${draft.name || "row"}`}
              >
                <Button.LeftIcon>
                  <Trash2 className="size-4" />
                </Button.LeftIcon>
              </Button>
            </RequireScope>
          )}
        </Stack>

        {draft.source === "static" ? (
          <div>
            <Type small muted className="mb-1">
              Static value
            </Type>
            <Input
              value={draft.staticValue}
              disabled={readOnly}
              onChange={(value) => onChange({ ...draft, staticValue: value })}
              onFocus={(event) => {
                // Editing a redacted secret should replace it, not append to
                // the `***` placeholder. Select it so the first keystroke wins.
                if (draft.staticValue === REDACTED_SECRET) {
                  event.currentTarget.select();
                }
              }}
              placeholder="Bearer …"
              type={showRevealToggle && !revealed ? "password" : "text"}
              className={showRevealToggle ? "pr-10" : undefined}
            >
              {showRevealToggle ? (
                <button
                  type="button"
                  onClick={() => setRevealed((current) => !current)}
                  className="text-muted-foreground hover:text-foreground absolute top-2.5 right-3"
                  aria-label={revealed ? "Hide value" : "Show value"}
                >
                  {revealed ? (
                    <EyeOff className="size-4" />
                  ) : (
                    <Eye className="size-4" />
                  )}
                </button>
              ) : null}
            </Input>
          </div>
        ) : null}

        {draft.source === "request" ? (
          <div>
            <Type small muted className="mb-1">
              Inbound request header
            </Type>
            <Input
              value={draft.valueFromRequestHeader}
              disabled={readOnly}
              onChange={(value) =>
                onChange({ ...draft, valueFromRequestHeader: value })
              }
              placeholder="X-Forwarded-Authorization"
            />
          </div>
        ) : null}

        <Stack direction="horizontal" gap={4}>
          <label className="flex items-center gap-2">
            <Checkbox
              checked={draft.isRequired}
              disabled={readOnly}
              onCheckedChange={(checked) =>
                onChange({ ...draft, isRequired: checked === true })
              }
            />
            <Type small>Required</Type>
          </label>
          {draft.source === "static" ? (
            <label className="flex items-center gap-2">
              <Checkbox
                checked={draft.isSecret}
                disabled={readOnly}
                onCheckedChange={(checked) =>
                  onChange({ ...draft, isSecret: checked === true })
                }
              />
              <Type small>Secret</Type>
            </label>
          ) : null}
        </Stack>
      </Stack>
    </div>
  );
}
