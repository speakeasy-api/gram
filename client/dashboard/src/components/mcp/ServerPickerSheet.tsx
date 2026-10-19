import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Checkbox } from "@/components/ui/Checkbox";
import { SearchBar } from "@/components/ui/SearchBar";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/Sheet";
import { Text } from "@/components/ui/Text";
import { Loader2 } from "lucide-react";
import { useRef, useState, type ReactNode } from "react";
import { toast } from "sonner";

export interface ServerPickerOption {
  value: string;
  label: string;
  icon: ReactNode;
  slug?: string;
  kindLabel: string;
  badge?: string;
  // Shown under the row when it is disabled; read to screen readers otherwise.
  description: string;
  disabled: boolean;
}

export interface ServerPickerResult {
  key: string;
  error?: string;
}

/**
 * Side sheet for adding several existing MCP servers to a container (a gateway
 * or a plugin) in one go: search, multi-select, per-row failures, and an
 * optional "create a new server" section above the list.
 */
export function ServerPickerSheet({
  open,
  onOpenChange,
  title,
  description,
  options,
  isLoading,
  loadFailed,
  onRetryLoad,
  notices,
  partiallyLoading = false,
  hideEmptyState = false,
  emptyMessage,
  canWrite,
  permissionMessage,
  adding = false,
  onSubmit,
  announceAdded = (count) =>
    toast.success(`${count} ${count === 1 ? "server" : "servers"} added`),
  renderCreateSection,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description: string;
  options: ServerPickerOption[];
  isLoading: boolean;
  loadFailed: boolean;
  onRetryLoad: () => void;
  // Partial-load notices rendered above the list (e.g. one source failed).
  notices?: ReactNode;
  // One source is still loading, so "Select all" would miss its servers.
  partiallyLoading?: boolean;
  // Suppress the empty state while a partial load may still add options.
  hideEmptyState?: boolean;
  emptyMessage: string;
  canWrite: boolean;
  permissionMessage: string;
  adding?: boolean;
  // Resolves one result per submitted value; a missing result is a failure.
  onSubmit: (values: string[]) => Promise<ServerPickerResult[]>;
  // Called once every selected server was added; toasts by default.
  announceAdded?: (count: number) => void;
  renderCreateSection?: (context: {
    busy: boolean;
    // Pass the dropdown's outside-pointer event so dismissing the menu does
    // not also dismiss the sheet.
    onMenuPointerDownOutside: (event: Event) => void;
  }) => ReactNode;
}): JSX.Element {
  const [selected, setSelected] = useState<string[]>([]);
  const [search, setSearch] = useState("");
  const searchContainerRef = useRef<HTMLLabelElement>(null);
  const creationMenuOutsideEventRef = useRef<Event | null>(null);
  const [results, setResults] = useState<ServerPickerResult[]>([]);
  const [submitting, setSubmitting] = useState(false);
  const submittingRef = useRef(false);
  const busy = adding || submitting;
  const query = search.trim().toLowerCase();
  const visibleOptions = options.filter((option) =>
    `${option.label} ${option.slug ?? ""}`.toLowerCase().includes(query),
  );
  const selectedValues = options
    .filter((option) => selected.includes(option.value) && !option.disabled)
    .map((option) => option.value);
  const selectableValues = visibleOptions
    .filter((option) => !option.disabled)
    .map((option) => option.value);
  const allSelected =
    selectableValues.length > 0 &&
    selectableValues.every((value) => selectedValues.includes(value));
  const canSelectAll =
    !isLoading &&
    !loadFailed &&
    !partiallyLoading &&
    selectableValues.length > 0;
  const toggleSelectAll = () =>
    setSelected((previous) => {
      const rest = previous.filter(
        (value) => !selectableValues.includes(value),
      );
      const everySelected = selectableValues.every((value) =>
        previous.includes(value),
      );
      return everySelected ? rest : [...rest, ...selectableValues];
    });
  const submit = async () => {
    if (
      submittingRef.current ||
      busy ||
      !canWrite ||
      isLoading ||
      loadFailed ||
      selectedValues.length === 0
    )
      return;
    submittingRef.current = true;
    setSubmitting(true);
    try {
      const returned = await onSubmit(selectedValues);
      // Missing results are not successes (including a rejected duplicate batch).
      const next = selectedValues.map(
        (key) =>
          returned.find((result) => result.key === key) ?? {
            key,
            error: "Couldn't confirm this server was added. Try again.",
          },
      );
      const failures = next.filter((result) => result.error);
      setResults(failures);
      setSelected(failures.map((result) => result.key));
      if (failures.length === 0) {
        announceAdded(next.length);
        // Bypass the user-dismissal busy guard after the batch has settled.
        onOpenChange(false);
      }
    } catch (error) {
      setResults(
        selectedValues.map((key) => ({
          key,
          error:
            error instanceof Error
              ? error.message
              : "Couldn't add this server. Try again.",
        })),
      );
    } finally {
      submittingRef.current = false;
      setSubmitting(false);
    }
  };
  return (
    <Sheet
      open={open}
      onOpenChange={(value) => {
        if (!busy) onOpenChange(value);
      }}
    >
      <SheetContent
        className="flex w-full flex-col gap-0 p-0 sm:max-w-xl"
        onPointerDownOutside={(event) => {
          // Dialog defers dismissal until click, after the menu has closed.
          // Consume only the pointer interaction that dismissed the menu.
          if (
            event.detail.originalEvent === creationMenuOutsideEventRef.current
          ) {
            event.preventDefault();
          }
        }}
        onEscapeKeyDown={(event) => {
          if (
            search &&
            document.activeElement ===
              searchContainerRef.current?.querySelector("input")
          ) {
            event.preventDefault();
            setSearch("");
          }
        }}
      >
        <SheetHeader className="px-6 pt-6 pb-4">
          <SheetTitle>{title}</SheetTitle>
          <SheetDescription>{description}</SheetDescription>
        </SheetHeader>
        <div className="flex-1 space-y-4 overflow-y-auto px-6 pb-6">
          {renderCreateSection?.({
            busy,
            onMenuPointerDownOutside: (event) => {
              creationMenuOutsideEventRef.current = event;
            },
          })}
          <section
            aria-labelledby="add-existing-server"
            className={
              renderCreateSection ? "space-y-4 border-t pt-4" : "space-y-4"
            }
          >
            <div className="flex items-center justify-between gap-3">
              <Text as="h3" id="add-existing-server" variant="subheading">
                Add existing servers
              </Text>
              {canSelectAll && (
                <Button
                  variant="tertiary"
                  size="sm"
                  disabled={busy}
                  onClick={toggleSelectAll}
                >
                  <Button.Text>
                    {allSelected ? "Clear selection" : "Select all"}
                  </Button.Text>
                </Button>
              )}
            </div>
            <label ref={searchContainerRef} className="block">
              <span className="sr-only">Search existing servers</span>
              <SearchBar
                value={search}
                onChange={setSearch}
                placeholder="Search by name or slug"
                disabled={busy}
              />
            </label>
            {notices}
            {isLoading ? (
              <Text muted>Loading servers…</Text>
            ) : loadFailed ? (
              <div role="alert" className="space-y-2">
                <Text>
                  Couldn't load all servers. Retry to refresh the list.
                </Text>
                <Button
                  variant="secondary"
                  onClick={onRetryLoad}
                  disabled={busy}
                >
                  <Button.Text>Retry loading</Button.Text>
                </Button>
              </div>
            ) : options.length === 0 &&
              hideEmptyState ? null : options.length === 0 ? (
              <div className="bg-muted/20 flex min-h-24 items-center justify-center border border-dashed px-6 py-8 text-center">
                <Text muted>{emptyMessage}</Text>
              </div>
            ) : (
              <ul aria-label="Existing servers" className="space-y-2">
                {visibleOptions.length === 0 ? (
                  <li className="p-3">
                    <Text muted>
                      No matching servers. Try another search
                      {renderCreateSection ? " or add a new server." : "."}
                    </Text>
                  </li>
                ) : (
                  visibleOptions.map((option) => {
                    const error = results.find(
                      (result) => result.key === option.value,
                    )?.error;
                    return (
                      <li key={option.value}>
                        {/* Native label activation toggles the checkbox once for
                            the whole row, without a second bubbling click handler. */}
                        <label
                          className={`border-border/60 flex items-center gap-3 border px-3 py-2.5 ${busy || option.disabled ? "cursor-not-allowed" : "hover:border-border hover:bg-muted/40 cursor-pointer"}`}
                        >
                          {option.icon}
                          <span className="flex min-w-0 flex-1 flex-col">
                            <Text
                              as="span"
                              className="truncate text-sm font-medium"
                            >
                              {option.label}
                            </Text>
                            <span className="flex items-center gap-2">
                              {option.slug && (
                                <Text
                                  as="span"
                                  muted
                                  className="truncate font-mono text-xs"
                                >
                                  {option.slug}
                                </Text>
                              )}
                              <Text as="span" muted className="text-xs">
                                {option.kindLabel}
                              </Text>
                            </span>
                            <span
                              id={`${option.value}-description`}
                              className={
                                option.disabled
                                  ? "text-muted-foreground text-xs"
                                  : "sr-only"
                              }
                            >
                              {option.description}
                            </span>
                            {error && (
                              <span
                                role="alert"
                                className="text-destructive text-xs"
                              >
                                {`${option.label}: ${error}`}
                              </span>
                            )}
                          </span>
                          {option.badge && (
                            <Badge variant="warning">
                              <Badge.Text>{option.badge}</Badge.Text>
                            </Badge>
                          )}
                          <Checkbox
                            aria-label={option.label}
                            aria-describedby={`${option.value}-description`}
                            checked={selectedValues.includes(option.value)}
                            disabled={busy || option.disabled}
                            onCheckedChange={(checked) => {
                              if (busy || option.disabled) return;
                              setSelected((previous) =>
                                checked
                                  ? [
                                      ...previous.filter(
                                        (value) => value !== option.value,
                                      ),
                                      option.value,
                                    ]
                                  : previous.filter(
                                      (value) => value !== option.value,
                                    ),
                              );
                            }}
                          />
                        </label>
                      </li>
                    );
                  })
                )}
              </ul>
            )}
            {!canWrite && (
              <Text muted small>
                {permissionMessage}
              </Text>
            )}
            {(busy || options.length > 0) && (
              <Button
                disabled={
                  busy ||
                  !canWrite ||
                  isLoading ||
                  loadFailed ||
                  selectedValues.length === 0
                }
                aria-busy={busy}
                onClick={() => void submit()}
              >
                {busy && (
                  <Button.LeftIcon>
                    <Loader2 className="size-4 animate-spin" aria-hidden />
                  </Button.LeftIcon>
                )}
                <Button.Text>Add selected servers</Button.Text>
              </Button>
            )}
            <div aria-live="polite" className="space-y-2">
              {busy && <Text muted>Adding selected servers…</Text>}
            </div>
          </section>
        </div>
      </SheetContent>
    </Sheet>
  );
}
