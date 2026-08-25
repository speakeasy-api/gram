import { useEffect, useRef, type CSSProperties, type JSX } from "react";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { PlusIcon, Trash2Icon } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  estimateSTokens,
  sumEstimates,
  type Estimate,
} from "@/lib/stoken/calculator";
import { formatTokenCount, parseTokenCount } from "@/lib/stoken/token-count";
import {
  rowsFromSearch,
  searchFromRows,
  type ProviderKey,
  type ProviderRow,
} from "@/lib/stoken/url-state";

// Ported from speakeasy-api/stoken-estimator. The methodology, the presets,
// and the copy are that worksheet's; the markup is rebuilt on the admin's
// primitives so it reads as one app next to the organizations list.

const ROUTE_ID = "/stoken-calculator";

type ProviderPreset = {
  label: string;
  tokenizerMin: string;
  tokenizerMax: string;
};

// Presets are editable business assumptions, not vendor facts: picking a
// provider writes them into the row, and the operator can change them there.
const PROVIDER_PRESETS: Record<ProviderKey, ProviderPreset> = {
  openai: { label: "OpenAI", tokenizerMin: "1.00", tokenizerMax: "1.00" },
  anthropic: {
    label: "Anthropic",
    tokenizerMin: "1.20",
    tokenizerMax: "1.60",
  },
  gemini: {
    label: "Google Gemini",
    tokenizerMin: "1.00",
    tokenizerMax: "1.00",
  },
  other: { label: "Other", tokenizerMin: "1.00", tokenizerMax: "1.00" },
};

// The picker's order. `other` last, because it is the one that asks for more.
const PROVIDER_KEYS: ProviderKey[] = ["openai", "anthropic", "gemini", "other"];

const integerFormatter = new Intl.NumberFormat("en-US", {
  maximumFractionDigits: 0,
});

// Which of the two errors `estimateSTokens` reports, told apart by prefix: the
// calculator has one message per field and the row shows each under its own.
const TOKEN_COUNT_ERROR_PREFIX = "Enter a whole monthly token count";

type RowCalculation = {
  estimate: Estimate | null;
  tokenError: string | null;
  tokenizerError: string | null;
};

const EMPTY_CALCULATION: RowCalculation = {
  estimate: null,
  tokenError: null,
  tokenizerError: null,
};

function createProviderRow(rows: ProviderRow[]): ProviderRow {
  const ids = new Set(rows.map((row) => row.id));
  let nextId = 1;
  while (ids.has(`row-${nextId}`)) nextId += 1;

  const preset = PROVIDER_PRESETS.openai;
  return {
    id: `row-${nextId}`,
    provider: "openai",
    customName: "",
    providerTokens: "",
    tokenizerMin: preset.tokenizerMin,
    tokenizerMax: preset.tokenizerMax,
  };
}

function providerName(row: ProviderRow): string {
  if (row.provider === "other" && row.customName.trim()) {
    return row.customName.trim();
  }
  return PROVIDER_PRESETS[row.provider].label;
}

function formatRange(estimate: Estimate): string {
  return `${formatTokenCount(estimate.low)}–${formatTokenCount(estimate.high)}`;
}

function formatExactRange(estimate: Estimate): string {
  return `${integerFormatter.format(estimate.low)}–${integerFormatter.format(estimate.high)}`;
}

function calculateProviderRow(row: ProviderRow): RowCalculation {
  if (row.providerTokens.trim() === "") return EMPTY_CALCULATION;

  const parsed = parseTokenCount(row.providerTokens);
  if (!parsed.ok) {
    return { ...EMPTY_CALCULATION, tokenError: parsed.error };
  }

  const result = estimateSTokens(parsed.value, {
    min: Number(row.tokenizerMin),
    max: Number(row.tokenizerMax),
  });
  if (!result.ok) {
    return result.error.startsWith(TOKEN_COUNT_ERROR_PREFIX)
      ? { ...EMPTY_CALCULATION, tokenError: result.error }
      : { ...EMPTY_CALCULATION, tokenizerError: result.error };
  }

  return { ...EMPTY_CALCULATION, estimate: result.value };
}

// The small caps heading each panel opens with, the same in all three.
function SectionIndex({ children }: { children: string }): JSX.Element {
  return (
    <p className="text-primary text-xs font-semibold tracking-wider uppercase">
      {children}
    </p>
  );
}

type ProviderRowEditorProps = {
  row: ProviderRow;
  index: number;
  calculation: RowCalculation;
  onChange: (id: string, patch: Partial<ProviderRow>) => void;
  onRemove: (id: string) => void;
};

function ProviderRowEditor({
  row,
  index,
  calculation,
  onChange,
  onRemove,
}: ProviderRowEditorProps): JSX.Element {
  const tokenInputId = `${row.id}-tokens`;
  const tokenHelperId = `${row.id}-tokens-helper`;
  const tokenErrorId = `${row.id}-tokens-error`;
  const customNameId = `${row.id}-custom-name`;
  const tokenizerMinId = `${row.id}-tokenizer-min`;
  const tokenizerMaxId = `${row.id}-tokenizer-max`;
  const tokenizerErrorId = `${row.id}-tokenizer-error`;
  const providerSelectId = `${row.id}-provider`;
  const name = providerName(row);
  const exactRange = calculation.estimate
    ? formatExactRange(calculation.estimate)
    : null;
  const min = Number(row.tokenizerMin);
  const max = Number(row.tokenizerMax);
  const minInvalid =
    Boolean(calculation.tokenizerError) &&
    (!Number.isFinite(min) || min <= 0 || (Number.isFinite(max) && min > max));
  const maxInvalid =
    Boolean(calculation.tokenizerError) &&
    (!Number.isFinite(max) || max <= 0 || (Number.isFinite(min) && min > max));

  return (
    <fieldset className="space-y-3 border-t px-4 py-4">
      <legend className="sr-only">
        Provider {index + 1}: {name}
      </legend>

      <div className="grid gap-4 md:grid-cols-[minmax(0,1fr)_minmax(0,1.4fr)_minmax(0,1fr)_auto] md:items-start">
        <div className="space-y-1.5">
          <label htmlFor={providerSelectId} className="text-sm font-medium">
            Provider
          </label>
          <Select
            value={row.provider}
            onValueChange={(value) => {
              const provider = value as ProviderKey;
              const preset = PROVIDER_PRESETS[provider];
              onChange(row.id, {
                provider,
                tokenizerMin: preset.tokenizerMin,
                tokenizerMax: preset.tokenizerMax,
              });
            }}
          >
            <SelectTrigger id={providerSelectId} className="w-full">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {PROVIDER_KEYS.map((key) => (
                <SelectItem key={key} value={key}>
                  {PROVIDER_PRESETS[key].label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>

          {row.provider === "other" ? (
            <div className="space-y-1.5 pt-1">
              <label htmlFor={customNameId} className="text-sm font-medium">
                Provider name
              </label>
              <Input
                id={customNameId}
                type="text"
                autoComplete="off"
                value={row.customName}
                onChange={(event) =>
                  onChange(row.id, { customName: event.target.value })
                }
              />
            </div>
          ) : null}
        </div>

        <div className="space-y-1.5">
          <label htmlFor={tokenInputId} className="text-sm font-medium">
            All-in monthly provider tokens
          </label>
          <Input
            id={tokenInputId}
            type="text"
            inputMode="text"
            autoComplete="off"
            autoCapitalize="characters"
            spellCheck={false}
            placeholder="120M"
            aria-describedby={`${tokenHelperId} ${tokenErrorId}`}
            aria-invalid={Boolean(calculation.tokenError)}
            value={row.providerTokens}
            onChange={(event) =>
              onChange(row.id, { providerTokens: event.target.value })
            }
          />
          <p id={tokenHelperId} className="sr-only">
            Enter an exact count or use K, M, B, or T (for example, 120M or
            1.5B). Use raw input + output token counts, not spend or
            cache-discount-weighted billing units. Do not add reasoning or
            cached-token breakdowns to a total that already includes them.
          </p>
          <p
            id={tokenErrorId}
            className="text-destructive text-xs"
            hidden={!calculation.tokenError}
          >
            {calculation.tokenError}
          </p>
        </div>

        <div className="space-y-1.5">
          <span className="block text-sm font-medium">
            Estimated s-token range
          </span>
          <output
            className="block py-2 text-base font-semibold tabular-nums"
            title={exactRange ? `${exactRange} s-tokens` : undefined}
            aria-label={
              exactRange ? `${exactRange} estimated s-tokens` : undefined
            }
          >
            {calculation.estimate ? formatRange(calculation.estimate) : "—"}
          </output>
        </div>

        <Button
          type="button"
          variant="ghost"
          size="icon"
          className="text-muted-foreground hover:text-destructive md:mt-6"
          aria-label={`Remove ${name} provider row ${index + 1}`}
          onClick={() => onRemove(row.id)}
        >
          <Trash2Icon />
        </Button>
      </div>

      <details className="group rounded-md border">
        <summary className="text-primary cursor-pointer list-none px-3 py-2 text-xs font-semibold select-none">
          Tokenizer assumption · {row.tokenizerMin || "—"}–
          {row.tokenizerMax || "—"}
        </summary>
        <div className="space-y-3 border-t px-3 py-3">
          <p className="text-muted-foreground text-xs">
            Editable business assumption, not a vendor fact. Higher ratios
            produce fewer s-tokens. The minimum ratio sets the high end; the
            maximum ratio sets the low end.
          </p>
          <div className="grid gap-3 sm:grid-cols-2">
            <div className="space-y-1.5">
              <label htmlFor={tokenizerMinId} className="text-sm font-medium">
                Minimum provider tokens per 1 s-token
              </label>
              <Input
                id={tokenizerMinId}
                type="number"
                min="0.01"
                step="0.01"
                inputMode="decimal"
                aria-describedby={tokenizerErrorId}
                aria-invalid={minInvalid}
                value={row.tokenizerMin}
                onChange={(event) =>
                  onChange(row.id, { tokenizerMin: event.target.value })
                }
              />
            </div>
            <div className="space-y-1.5">
              <label htmlFor={tokenizerMaxId} className="text-sm font-medium">
                Maximum provider tokens per 1 s-token
              </label>
              <Input
                id={tokenizerMaxId}
                type="number"
                min="0.01"
                step="0.01"
                inputMode="decimal"
                aria-describedby={tokenizerErrorId}
                aria-invalid={maxInvalid}
                value={row.tokenizerMax}
                onChange={(event) =>
                  onChange(row.id, { tokenizerMax: event.target.value })
                }
              />
            </div>
          </div>
          <p
            id={tokenizerErrorId}
            className="text-destructive text-xs"
            hidden={!calculation.tokenizerError}
          >
            {calculation.tokenizerError}
          </p>
        </div>
      </details>
    </fieldset>
  );
}

type ResultPanelProps = {
  total: Estimate;
  validRowCount: number;
  attentionCount: number;
};

function ResultPanel({
  total,
  validRowCount,
  attentionCount,
}: ResultPanelProps): JSX.Element {
  const attentionText = `${attentionCount} ${attentionCount === 1 ? "row needs" : "rows need"} attention`;
  const hasValidRows = validRowCount > 0;
  const exactRange = formatExactRange(total);
  const low = formatTokenCount(total.low);
  const high = formatTokenCount(total.high);
  const exactLow = integerFormatter.format(total.low);
  const exactHigh = integerFormatter.format(total.high);
  const lowPosition = total.high === 0 ? 0 : (total.low / total.high) * 100;
  const accessibleRange = `Low ${exactLow} s-tokens; high ${exactHigh} s-tokens`;
  const railStyle = { "--low-position": `${lowPosition}%` } as CSSProperties;
  const liveText = hasValidRows
    ? `${accessibleRange}. ${validRowCount} valid provider ${validRowCount === 1 ? "row" : "rows"}.${attentionCount ? ` ${attentionText}.` : ""}`
    : attentionCount
      ? `No usable provider rows. ${attentionText}.`
      : "Add provider usage to estimate s-tokens.";

  return (
    <aside
      className="h-fit rounded-lg border px-4 py-4"
      aria-labelledby="result-heading"
    >
      <div className="border-b pb-3">
        <SectionIndex>Combined envelope</SectionIndex>
        <h2 id="result-heading" className="text-lg font-semibold">
          Monthly s-token range
        </h2>
      </div>

      {hasValidRows ? (
        <div className="space-y-4 pt-4">
          <output
            className="block text-3xl font-semibold tabular-nums"
            title={`${exactRange} s-tokens`}
            aria-label={`${exactRange} s-tokens`}
          >
            {formatRange(total)}
          </output>

          {/* The rail: the low end sits at low/high along the track and the
              high end at the right edge, so the width of the fill is how
              wide the envelope is. */}
          <div
            role="img"
            aria-label={accessibleRange}
            title={accessibleRange}
            style={railStyle}
            className="space-y-2"
          >
            <div
              className="bg-muted relative h-2 rounded-full"
              aria-hidden="true"
            >
              <span className="bg-primary/60 absolute inset-y-0 right-0 left-(--low-position) rounded-full" />
              <span className="bg-primary absolute top-1/2 left-(--low-position) size-3 -translate-x-1/2 -translate-y-1/2 rounded-full" />
              <span className="bg-primary absolute top-1/2 right-0 size-3 translate-x-1/2 -translate-y-1/2 rounded-full" />
            </div>
            <div
              className="text-muted-foreground flex justify-between text-xs"
              aria-hidden="true"
            >
              <span>Low {low}</span>
              <span>High {high}</span>
            </div>
          </div>

          <dl className="space-y-2 text-sm">
            <div className="flex items-baseline justify-between gap-3">
              <dt className="text-muted-foreground">
                Low · 3× hidden · max tokenizer
              </dt>
              <dd
                className="font-medium tabular-nums"
                title={`${exactLow} s-tokens`}
                aria-label={`${exactLow} s-tokens`}
              >
                {low}
              </dd>
            </div>
            <div className="flex items-baseline justify-between gap-3">
              <dt className="text-muted-foreground">
                High · 2× hidden · min tokenizer
              </dt>
              <dd
                className="font-medium tabular-nums"
                title={`${exactHigh} s-tokens`}
                aria-label={`${exactHigh} s-tokens`}
              >
                {high}
              </dd>
            </div>
          </dl>
        </div>
      ) : (
        <p className="text-muted-foreground pt-4 text-sm">
          Add provider usage to estimate s-tokens.
        </p>
      )}

      <p
        className="text-destructive pt-3 text-xs"
        hidden={attentionCount === 0}
      >
        {attentionCount === 0 ? "" : attentionText}
      </p>
      <p className="sr-only" aria-live="polite" aria-atomic="true">
        {liveText}
      </p>
    </aside>
  );
}

function MethodSection(): JSX.Element {
  return (
    <section
      className="space-y-4 rounded-lg border px-4 py-4"
      aria-labelledby="method-heading"
    >
      <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_minmax(0,2fr)] lg:items-start">
        <div className="space-y-1">
          <SectionIndex>Method notes</SectionIndex>
          <h2 id="method-heading" className="text-lg font-semibold">
            How this estimate works
          </h2>
          <p className="text-muted-foreground text-sm">
            Start with <strong>P</strong>, the prospect’s all-in monthly
            provider total. Both adjustments divide; provider-counted components
            already included in P are never added again.
          </p>
        </div>

        <div
          className="grid gap-3 sm:grid-cols-2"
          aria-label="S-token endpoint equations"
        >
          <div className="space-y-2 rounded-md border px-3 py-3">
            <SectionIndex>Low endpoint</SectionIndex>
            <code className="block font-mono text-sm">
              S<sub>low</sub> = P ÷ 3 ÷ R<sub>max</sub>
            </code>
          </div>
          <div className="space-y-2 rounded-md border px-3 py-3">
            <SectionIndex>High endpoint</SectionIndex>
            <code className="block font-mono text-sm">
              S<sub>high</sub> = P ÷ 2 ÷ R<sub>min</sub>
            </code>
          </div>
        </div>
      </div>

      <div className="text-muted-foreground grid gap-3 border-t pt-4 text-sm md:grid-cols-2">
        <p>
          <strong className="text-foreground">Hidden-content divisor H.</strong>{" "}
          Dividing by 2–3 removes hidden reasoning, system, compaction, and
          other provider-counted content that is not scanned.
        </p>
        <p>
          <strong className="text-foreground">Tokenizer divisor R.</strong>{" "}
          Dividing by a ratio above 1 removes the provider tokenizer’s extra
          tokens for the same visible workload measured with{" "}
          <code className="font-mono">o200k_base</code>.
        </p>
      </div>

      <p className="flex flex-wrap items-baseline gap-3 border-t pt-4 text-sm">
        <SectionIndex>Anthropic example</SectionIndex>
        <code className="font-mono">
          120M provider tokens at R = 1.20–1.60 → 25M–50M s-tokens.
        </code>
      </p>

      <p className="border-t pt-4 text-sm font-medium">
        Quote estimate, not measured usage. This band combines the outer
        hidden-content and tokenizer scenarios; it is not a statistical
        confidence interval and has no implied midpoint. Actual conversion
        varies by model, tokenizer version, language, and workload.
      </p>

      <details className="border-t pt-4">
        <summary className="cursor-pointer text-sm font-semibold select-none">
          How to calibrate the assumptions
        </summary>
        <div className="text-muted-foreground space-y-3 pt-3 text-sm">
          <p>
            Match the exact payload occurrences sent to the scanner over the
            same representative time window and model/workload mix. Use ratios
            of sums, not averages of per-message or per-session ratios. The
            worksheet reports a combined scenario envelope, not a statistical
            confidence interval.
          </p>
          <code className="block font-mono text-xs">
            H<sub>calibrated</sub> = Σ all-in provider tokens ÷ Σ provider-token
            count of scanner payloads
          </code>
          <code className="block font-mono text-xs">
            R<sub>calibrated</sub> = Σ provider-token count of those scanner
            payloads ÷ Σ o200k_base count of those same payloads
          </code>
          <p>
            Keep H and R separate only when H was measured in provider-token
            units. If telemetry provides provider total P and directly measured
            s-tokens S, use the combined factor C = P ÷ S and S = P ÷ C. That
            direct P / S factor replaces—rather than compounds with—the
            tokenizer ratio; dividing by R again would double-correct tokenizer
            differences.
          </p>
        </div>
      </details>
    </section>
  );
}

/** A worksheet turning provider-reported monthly usage into an s-token range. */
export function StokenCalculator(): JSX.Element {
  const search = useSearch({ from: ROUTE_ID });
  const navigate = useNavigate({ from: ROUTE_ID });
  const rows = rowsFromSearch(search);
  const pendingFocusId = useRef<string | null>(null);

  // Adding or removing a row moves focus to the row that took its place, and
  // the element does not exist until the URL change has rendered.
  useEffect(() => {
    if (!pendingFocusId.current) return;
    document.getElementById(pendingFocusId.current)?.focus();
    pendingFocusId.current = null;
  }, [rows]);

  const calculations = rows.map(calculateProviderRow);
  const estimates: Estimate[] = [];
  let attentionCount = 0;
  for (const [index, calculation] of calculations.entries()) {
    if (calculation.estimate) {
      estimates.push(calculation.estimate);
    } else if (rows[index]?.providerTokens.trim() !== "") {
      attentionCount += 1;
    }
  }
  const total = sumEstimates(estimates);

  // Replaced, not pushed: every keystroke writes the URL, and Back should
  // leave the calculator rather than undo one character.
  function setRows(next: ProviderRow[]): void {
    void navigate({ search: searchFromRows(next), replace: true });
  }

  function updateRow(id: string, patch: Partial<ProviderRow>): void {
    setRows(rows.map((row) => (row.id === id ? { ...row, ...patch } : row)));
  }

  function addProvider(): void {
    const row = createProviderRow(rows);
    pendingFocusId.current = `${row.id}-provider`;
    setRows([...rows, row]);
  }

  function removeProvider(id: string): void {
    const index = rows.findIndex((row) => row.id === id);
    if (index === -1) return;

    const remaining = rows.filter((row) => row.id !== id);
    const nextRow = remaining[Math.min(index, remaining.length - 1)];
    pendingFocusId.current = nextRow
      ? `${nextRow.id}-provider`
      : "add-provider";
    setRows(remaining);
  }

  return (
    <div className="space-y-6">
      <div className="grid gap-6 lg:grid-cols-[minmax(0,2fr)_minmax(0,1fr)]">
        <section
          className="rounded-lg border"
          aria-labelledby="worksheet-heading"
        >
          <div className="grid gap-3 px-4 py-4 md:grid-cols-[minmax(0,1fr)_minmax(0,2fr)]">
            <div>
              <SectionIndex>Provider inputs</SectionIndex>
              <h2 id="worksheet-heading" className="text-lg font-semibold">
                Monthly usage
              </h2>
            </div>
            <div className="text-muted-foreground space-y-1 text-xs">
              <p className="font-medium">
                Presets are editable business assumptions, not vendor facts.
              </p>
              <p>
                Enter an exact count or use K, M, B, or T (for example, 120M or
                1.5B). Use raw input + output token counts, not spend or
                cache-discount-weighted billing units. Do not add reasoning or
                cached-token breakdowns to a total that already includes them.
              </p>
            </div>
          </div>

          {rows.map((row, index) => (
            <ProviderRowEditor
              key={row.id}
              row={row}
              index={index}
              calculation={calculations[index] ?? EMPTY_CALCULATION}
              onChange={updateRow}
              onRemove={removeProvider}
            />
          ))}
          <p
            className="text-muted-foreground border-t px-4 py-4 text-sm"
            hidden={rows.length !== 0}
          >
            No provider rows. Add provider usage to continue.
          </p>

          <footer className="flex flex-wrap items-center justify-between gap-3 border-t px-4 py-4">
            <div className="space-y-0.5">
              <span className="text-muted-foreground block text-xs">
                Combined monthly s-token range
              </span>
              <output
                className="block text-base font-semibold tabular-nums"
                title={
                  estimates.length
                    ? `${formatExactRange(total)} s-tokens`
                    : undefined
                }
                aria-label={
                  estimates.length
                    ? `${formatExactRange(total)} s-tokens`
                    : undefined
                }
              >
                {estimates.length ? formatRange(total) : "—"}
              </output>
            </div>
            <Button id="add-provider" type="button" onClick={addProvider}>
              <PlusIcon />
              Add provider
            </Button>
          </footer>
        </section>

        <ResultPanel
          total={total}
          validRowCount={estimates.length}
          attentionCount={attentionCount}
        />
      </div>

      <MethodSection />
    </div>
  );
}
