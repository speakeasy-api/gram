import { duplicateNameCounts } from "./signal-names";
import { AnyField } from "@/components/moon/any-field";
import { InputField } from "@/components/moon/input-field";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { TextArea } from "@/components/ui/Textarea";
import type {
  SigintSensor,
  SigintSensorMode,
} from "@gram/client/models/components/sigintsensor.js";
import type { SigintSignal } from "@gram/client/models/components/sigintsignal.js";
import { ArrowDown, ArrowUp, Plus, X } from "lucide-react";
import { useMemo, useState, type FormEvent } from "react";

export interface SensorDraft {
  name: string;
  description: string;
  instructions: string;
  mode: SigintSensorMode;
  signalIds: string[];
}

interface SensorEditorDialogProps {
  open: boolean;
  canWrite: boolean;
  sensor: SigintSensor | null;
  signals: SigintSignal[];
  catalogLoading: boolean;
  catalogError: boolean;
  hasMoreSignals: boolean;
  loadingMoreSignals: boolean;
  pending: boolean;
  error: string | null;
  onLoadMoreSignals: () => void;
  onRetrySignals: () => void;
  onOpenChange: (open: boolean) => void;
  onSave: (draft: SensorDraft) => void;
}

const MODE_OPTIONS: Array<{
  value: SigintSensorMode;
  label: string;
  description: string;
}> = [
  {
    value: "multi_label",
    label: "Multi-label",
    description:
      "Signals apply independently. Membership is not capped by this mode.",
  },
  {
    value: "exclusive",
    label: "Exclusive",
    description: "Signals compete as one choice. Up to 255 signals.",
  },
  {
    value: "ordered_score",
    label: "Ordered score",
    description:
      "Order defines zero-based levels from low to high. Up to 10 signals.",
  },
];

function signalLimit(mode: SigintSensorMode): number {
  if (mode === "ordered_score") return 10;
  if (mode === "exclusive") return 255;
  return Number.POSITIVE_INFINITY;
}

function validateName(name: string): string | null {
  const length = Array.from(name.trim()).length;
  if (length === 0) return "Enter a name.";
  if (length > 200) return "Use 200 characters or fewer.";
  return null;
}

function signalLabel(
  signal: SigintSignal,
  counts: Map<string, number>,
): string {
  return (counts.get(signal.name) ?? 0) > 1
    ? `${signal.name} (${signal.id})`
    : signal.name;
}

export function SensorEditorDialog({
  open,
  canWrite,
  sensor,
  signals,
  catalogLoading,
  catalogError,
  hasMoreSignals,
  loadingMoreSignals,
  pending,
  error,
  onLoadMoreSignals,
  onRetrySignals,
  onOpenChange,
  onSave,
}: SensorEditorDialogProps): JSX.Element {
  const [name, setName] = useState(sensor?.name ?? "");
  const [description, setDescription] = useState(sensor?.description ?? "");
  const [instructions, setInstructions] = useState(sensor?.instructions ?? "");
  const [mode, setMode] = useState<SigintSensorMode>(
    sensor?.mode ?? "multi_label",
  );
  const [signalIds, setSignalIds] = useState<string[]>(sensor?.signalIds ?? []);
  const [search, setSearch] = useState("");
  const [submitted, setSubmitted] = useState(false);

  const signalsById = useMemo(
    () => new Map(signals.map((signal) => [signal.id, signal])),
    [signals],
  );
  const nameCounts = useMemo(() => duplicateNameCounts(signals), [signals]);
  const selected = new Set(signalIds);
  const needle = search.trim().toLocaleLowerCase();
  const available = signals.filter(
    (signal) =>
      !selected.has(signal.id) &&
      (needle.length === 0 ||
        signal.name.toLocaleLowerCase().includes(needle) ||
        signal.id.toLocaleLowerCase().includes(needle)),
  );
  const nameValidation = validateName(name);
  const tooManySignals = signalIds.length > signalLimit(mode);
  const selectedMode = MODE_OPTIONS.find((option) => option.value === mode)!;

  const move = (index: number, offset: -1 | 1): void => {
    const destination = index + offset;
    if (destination < 0 || destination >= signalIds.length) return;
    setSignalIds((current) => {
      const next = [...current];
      [next[index], next[destination]] = [next[destination]!, next[index]!];
      return next;
    });
  };

  const submit = (event: FormEvent<HTMLFormElement>): void => {
    event.preventDefault();
    setSubmitted(true);
    if (!canWrite || pending || nameValidation || tooManySignals) return;
    onSave({
      name: name.trim(),
      description,
      instructions,
      mode,
      signalIds,
    });
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!pending) onOpenChange(next);
      }}
    >
      <Dialog.Content className="max-h-[92vh] overflow-y-auto sm:max-w-4xl">
        <Dialog.Header>
          <Dialog.Title>
            {sensor ? "Edit sensor" : "Create sensor"}
          </Dialog.Title>
          <Dialog.Description>
            Compose reusable signals into configuration for a classifier. Empty
            and one-signal sensors are valid drafts; saving does not activate
            analysis.
          </Dialog.Description>
        </Dialog.Header>
        <form onSubmit={submit} className="space-y-6">
          <div className="grid gap-5 md:grid-cols-2">
            <InputField
              label="Name"
              value={name}
              onChange={(event) => setName(event.target.value)}
              required
              autoFocus
              error={submitted ? nameValidation : null}
              hint="A trimmed name between 1 and 200 Unicode characters."
            />
            <AnyField
              label="Mode"
              optionality="hidden"
              hint={selectedMode.description}
              render={(props) => (
                <Select
                  value={mode}
                  onValueChange={(value) => setMode(value as SigintSensorMode)}
                >
                  <SelectTrigger {...props} className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {MODE_OPTIONS.map((option) => (
                      <SelectItem
                        key={option.value}
                        value={option.value}
                        description={option.description}
                      >
                        {option.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              )}
            />
          </div>
          <div className="grid gap-5 md:grid-cols-2">
            <AnyField
              label="Description"
              hint="Optional context for people configuring this sensor."
              render={(props) => (
                <TextArea
                  {...props}
                  value={description}
                  onChange={setDescription}
                  rows={4}
                />
              )}
            />
            <AnyField
              label="Instructions"
              hint="Optional framing instructions for classification."
              render={(props) => (
                <TextArea
                  {...props}
                  value={instructions}
                  onChange={setInstructions}
                  rows={4}
                />
              )}
            />
          </div>

          <section
            className="space-y-3"
            aria-labelledby="sensor-signals-heading"
          >
            <div>
              <h3 id="sensor-signals-heading" className="text-eyebrow">
                Ordered signal membership
              </h3>
              <p className="text-muted-foreground mt-1 text-sm">
                Reuse any signal in the project. {selectedMode.description}
              </p>
            </div>
            {tooManySignals ? (
              <Alert variant="error">
                {selectedMode.label} sensors support at most {signalLimit(mode)}{" "}
                signals. Remove signals or choose another mode.
              </Alert>
            ) : null}
            <div className="grid min-h-72 gap-4 md:grid-cols-2">
              <div className="border p-3">
                <InputField
                  label="Available signals"
                  value={search}
                  onChange={(event) => setSearch(event.target.value)}
                  placeholder="Search by name or ID"
                  optionality="hidden"
                />
                <div className="mt-3 max-h-64 space-y-1 overflow-y-auto">
                  {catalogLoading ? (
                    <p className="text-muted-foreground py-6 text-center text-sm">
                      Loading signals…
                    </p>
                  ) : null}
                  {catalogError ? (
                    <div className="space-y-2 py-4 text-center">
                      <p className="text-destructive text-sm">
                        Unable to load signals.
                      </p>
                      <Button
                        type="button"
                        variant="secondary"
                        size="sm"
                        onClick={onRetrySignals}
                      >
                        Retry
                      </Button>
                    </div>
                  ) : null}
                  {!catalogLoading &&
                  !catalogError &&
                  available.length === 0 ? (
                    <p className="text-muted-foreground py-6 text-center text-sm">
                      {search
                        ? "No matching available signals."
                        : "No available signals."}
                    </p>
                  ) : null}
                  {available.map((signal) => {
                    const atLimit = signalIds.length >= signalLimit(mode);
                    return (
                      <div
                        key={signal.id}
                        className="flex items-center justify-between gap-2 border-b py-2 last:border-b-0"
                      >
                        <span
                          className="min-w-0 truncate text-sm"
                          title={signalLabel(signal, nameCounts)}
                        >
                          {signalLabel(signal, nameCounts)}
                        </span>
                        <Button
                          type="button"
                          variant="tertiary"
                          size="xs"
                          aria-label={`Add ${signalLabel(signal, nameCounts)}`}
                          disabled={atLimit}
                          onClick={() =>
                            setSignalIds((current) => [...current, signal.id])
                          }
                        >
                          <Plus />
                          Add
                        </Button>
                      </div>
                    );
                  })}
                </div>
                {hasMoreSignals ? (
                  <Button
                    type="button"
                    variant="secondary"
                    size="sm"
                    className="mt-3 w-full"
                    disabled={loadingMoreSignals}
                    onClick={onLoadMoreSignals}
                  >
                    {loadingMoreSignals ? "Loading…" : "Load more signals"}
                  </Button>
                ) : null}
              </div>

              <div className="border p-3">
                <h4 className="text-eyebrow">Selected signals</h4>
                <p className="text-muted-foreground mt-1 text-sm">
                  {signalIds.length} selected
                </p>
                <div className="mt-3 max-h-72 space-y-1 overflow-y-auto">
                  {signalIds.length === 0 ? (
                    <p className="text-muted-foreground py-8 text-center text-sm">
                      No signals selected. You can save this draft empty.
                    </p>
                  ) : null}
                  {signalIds.map((id, index) => {
                    const signal = signalsById.get(id);
                    const label = signal ? signalLabel(signal, nameCounts) : id;
                    return (
                      <div
                        key={id}
                        className="flex items-center gap-2 border-b py-2 last:border-b-0"
                      >
                        {mode === "ordered_score" ? (
                          <span
                            className="w-6 shrink-0 font-mono text-sm"
                            aria-label={`Score level ${index}`}
                          >
                            {index}
                          </span>
                        ) : null}
                        <span
                          className="min-w-0 flex-1 truncate text-sm"
                          title={label}
                        >
                          {label}
                        </span>
                        <Button
                          type="button"
                          variant="tertiary"
                          size="xs"
                          aria-label={`Move ${label} up`}
                          disabled={index === 0}
                          onClick={() => move(index, -1)}
                        >
                          <ArrowUp />
                        </Button>
                        <Button
                          type="button"
                          variant="tertiary"
                          size="xs"
                          aria-label={`Move ${label} down`}
                          disabled={index === signalIds.length - 1}
                          onClick={() => move(index, 1)}
                        >
                          <ArrowDown />
                        </Button>
                        <Button
                          type="button"
                          variant="tertiary"
                          size="xs"
                          aria-label={`Remove ${label}`}
                          onClick={() =>
                            setSignalIds((current) =>
                              current.filter((signalId) => signalId !== id),
                            )
                          }
                        >
                          <X />
                        </Button>
                      </div>
                    );
                  })}
                </div>
              </div>
            </div>
          </section>

          {error ? <Alert variant="error">{error}</Alert> : null}
          <Dialog.Footer>
            <Button
              type="button"
              variant="secondary"
              onClick={() => onOpenChange(false)}
              disabled={pending}
            >
              Cancel
            </Button>
            <Button
              type="submit"
              disabled={
                !canWrite ||
                pending ||
                (submitted && (!!nameValidation || tooManySignals))
              }
            >
              {pending ? "Saving…" : "Save sensor"}
            </Button>
          </Dialog.Footer>
        </form>
      </Dialog.Content>
    </Dialog>
  );
}
