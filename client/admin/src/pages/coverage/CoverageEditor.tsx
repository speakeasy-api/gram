import {
  accountFact,
  accountTypes,
  accountLabels,
  accountEligibility,
  updateAccountEligibility,
  withoutAccountConditions,
  updateAccountNotes,
  type AccountEligibility,
  type AccountFilter,
} from "./accounts";
import { useId, useState, type JSX } from "react";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  emptyMapping,
  getFact,
  mappingKey,
  methodReference,
  statusLabels,
  symbols,
  unknown,
  type Capability,
  type Draft,
  type Fact,
  type Mapping,
  type Method,
  type Product,
} from "./model";
import "./support-status.css";

export function Choice({
  id,
  label,
  value,
  options,
  onChange,
}: {
  id?: string;
  label: string;
  value: string;
  options: { value: string; label: string }[];
  onChange: (value: string) => void;
}): JSX.Element {
  return (
    <Select value={value} onValueChange={onChange}>
      <SelectTrigger id={id} aria-label={label} className="w-full">
        <SelectValue placeholder={label} />
      </SelectTrigger>
      <SelectContent>
        {options.map((option) => (
          <SelectItem key={option.value} value={option.value}>
            {option.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

function withoutSourceAnnotation(value: string): string {
  return value.replace(/\bSource cell:\s*(?:✅|❌|☠️?|✓|×|--)?\s*;?\s*/gi, "");
}

export function FactEditor({
  fact,
  capability,
  description,
  onChange,
}: {
  fact: Fact;
  capability: Capability;
  description: string;
  onChange: (fact: Fact) => void;
}): JSX.Element {
  const statusId = useId();
  return (
    <div className="space-y-3">
      <div className="space-y-1">
        <label htmlFor={statusId} className="text-sm font-medium">
          {capability.name} coverage
        </label>
        <p className="text-muted-foreground text-xs">{description}</p>
      </div>
      <Choice
        id={statusId}
        label={`${capability.name} coverage`}
        value={fact.status}
        options={Object.entries(statusLabels).map(([value, label]) => ({
          value,
          label,
        }))}
        onChange={(status) =>
          onChange({ ...fact, status: status as Fact["status"] })
        }
      />
      <Textarea
        aria-label="Coverage limitations"
        rows={3}
        placeholder="Limitations, mechanism, or evidence…"
        value={withoutSourceAnnotation(fact.note)}
        onChange={(event) => onChange({ ...fact, note: event.target.value })}
      />
      <label className="flex items-center gap-2 text-sm">
        <input
          type="checkbox"
          checked={fact.verify}
          onChange={(event) =>
            onChange({ ...fact, verify: event.target.checked })
          }
        />
        Needs verification
      </label>
      {fact.status === "partial" && !fact.note.trim() && (
        <p className="text-destructive text-xs">
          Describe what is and isn’t covered.
        </p>
      )}
    </div>
  );
}

export function MethodEditor({
  method,
  product,
  capability,
  draft,
  onSave,
  embedded = false,
}: {
  embedded?: boolean;
  method: Method;
  product?: Product;
  capability: Capability;
  draft: Draft;
  onSave: (draft: Draft) => Promise<boolean>;
}): JSX.Element {
  const key = product ? mappingKey(method.id, product.id) : "";
  const [mapping, setMapping] = useState<Mapping>(
    draft.mappings[key] ?? emptyMapping,
  );
  const [reference, setReference] = useState(
    draft.references[method.id]?.[capability.id] ??
      method.facts[capability.id] ??
      unknown,
  );
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState(false);
  const fact = product ? getFact(mapping, capability.id, reference) : reference;
  const valid = fact.status !== "partial" || !!fact.note.trim();
  function updateMapping(next: Mapping) {
    setMapping(next);
    setSaved(false);
  }
  async function save() {
    setSaving(true);
    const next = product
      ? { ...draft, mappings: { ...draft.mappings, [key]: mapping } }
      : {
          ...draft,
          references: {
            ...draft.references,
            [method.id]: {
              ...draft.references[method.id],
              [capability.id]: reference,
            },
          },
        };
    const success = await onSave(next);
    setSaved(success);
    setSaving(false);
  }
  return (
    <section
      className={
        embedded ? "space-y-4 px-4 pb-4" : "space-y-4 rounded-lg border p-4"
      }
    >
      <div>
        {!embedded && <h3 className="font-medium">{method.name}</h3>}
        {!product && (
          <p className="text-muted-foreground mt-1 text-xs">{method.plans}</p>
        )}
      </div>
      {product && (
        <>
          <Choice
            label={`Applicability of ${method.name}`}
            value={mapping.applicability}
            options={[
              { value: "unknown", label: "Applicability unknown" },
              { value: "applicable", label: `Applies to ${product.family}` },
              { value: "na", label: `Does not apply to ${product.family}` },
            ]}
            onChange={(value) =>
              updateMapping({
                ...mapping,
                applicability: value as Mapping["applicability"],
              })
            }
          />
          <fieldset className="space-y-2">
            <legend className="text-sm font-medium">Account eligibility</legend>
            <p className="text-muted-foreground text-xs">
              Which accounts can use {method.name} on {product.name}. Applies
              across this method’s capabilities on this platform.
            </p>
            <div className="grid grid-cols-3 gap-2">
              {accountTypes.map((type) => (
                <div key={type} className="space-y-1">
                  <span className="text-xs font-medium">
                    {accountLabels[type]}
                  </span>
                  <Choice
                    label={`${accountLabels[type]} account eligibility`}
                    value={accountEligibility(method, mapping.conditions, type)}
                    options={[
                      { value: "supported", label: "Eligible" },
                      { value: "unsupported", label: "Ineligible" },
                      { value: "unknown", label: "Unknown" },
                    ]}
                    onChange={(value) =>
                      updateMapping({
                        ...mapping,
                        conditions: updateAccountEligibility(
                          method,
                          mapping.conditions,
                          type,
                          value as AccountEligibility,
                        ),
                      })
                    }
                  />
                </div>
              ))}
            </div>
          </fieldset>
          <Textarea
            aria-label="OS and plan conditions"
            rows={3}
            placeholder="OS / plan conditions, e.g. macOS, Enterprise only"
            value={withoutSourceAnnotation(
              withoutAccountConditions(mapping.conditions),
            )}
            onChange={(event) =>
              updateMapping({
                ...mapping,
                conditions: updateAccountNotes(
                  mapping.conditions,
                  event.target.value,
                ),
              })
            }
          />
        </>
      )}
      {(!product || mapping.applicability === "applicable") && (
        <FactEditor
          fact={fact}
          capability={capability}
          description={`How well ${method.name} supports ${capability.name} ${product ? `on ${product.name}` : "across applicable platforms"}. This status applies to all eligible account types.`}
          onChange={(next) => {
            setSaved(false);
            if (product)
              setMapping({
                ...mapping,
                facts: { ...mapping.facts, [capability.id]: next },
              });
            else setReference(next);
          }}
        />
      )}
      {product && mapping.applicability !== "applicable" && (
        <p className="text-muted-foreground text-sm">
          Mark this method as applicable to edit its capabilities. Existing
          capability edits are retained when applicability changes.
        </p>
      )}
      {product && mapping.applicability === "applicable" && (
        <p className="text-muted-foreground text-xs">
          Coverage follows this method’s feature claims and platform conditions.
          Editing a feature saves an explicit override for this platform.
        </p>
      )}
      <div className="flex items-center justify-between gap-3">
        <Button
          size="sm"
          disabled={!valid || saving}
          onClick={() => void save()}
        >
          Save coverage
        </Button>
        <span role="status" className="text-muted-foreground text-xs">
          {saved ? "Saved to database" : ""}
        </span>
      </div>
    </section>
  );
}

const supportOrder: Record<Fact["status"], number> = {
  supported: 0,
  partial: 1,
  unknown: 2,
  unimplemented: 3,
  impossible: 4,
  na: 5,
};

export function MethodList({
  methods,
  product,
  capability,
  draft,
  onSave,
  account = "all",
}: {
  methods: Method[];
  account?: AccountFilter;
  product: Product;
  capability: Capability;
  draft: Draft;
  onSave: (draft: Draft) => Promise<boolean>;
}): JSX.Element {
  const entries = methods
    .map((method) => ({
      method,
      fact: accountFact(
        method,
        getFact(
          draft.mappings[mappingKey(method.id, product.id)] ?? emptyMapping,
          capability.id,
          methodReference(draft, method, capability.id),
        ),
        account,
        draft.mappings[mappingKey(method.id, product.id)]?.conditions,
      ),
    }))
    .sort((a, b) => supportOrder[a.fact.status] - supportOrder[b.fact.status]);
  return (
    <div className="space-y-3">
      <p className="text-muted-foreground text-xs">
        Supported methods first, followed by partial coverage and other methods.
      </p>
      {entries.map(({ method, fact }) => {
        const supported = fact.status === "supported";
        const partial = fact.status === "partial";
        return (
          <details
            key={`${method.id}/${product.id}/${capability.id}`}
            className={
              supported
                ? "border-foreground/30 rounded-lg border"
                : "border-border rounded-lg border"
            }
          >
            <summary
              className={`cursor-pointer rounded-lg px-4 py-3 ${supported || partial ? "text-foreground" : "text-muted-foreground bg-muted/40"}`}
            >
              <span className="inline-flex w-[calc(100%-1.25rem)] items-center justify-between gap-3 align-middle">
                <span className={supported ? "font-semibold" : "font-normal"}>
                  {method.name}
                </span>
                <span
                  data-support-status={
                    supported || partial ? fact.status : "unknown-method"
                  }
                  className="support-status shrink-0 rounded-sm px-2 py-1 text-xs"
                >
                  {symbols[fact.status]} {statusLabels[fact.status]}
                  {fact.verify ? "*" : ""}
                </span>
              </span>
            </summary>
            <MethodEditor
              embedded
              method={method}
              product={product}
              capability={capability}
              draft={draft}
              onSave={onSave}
            />
          </details>
        );
      })}
    </div>
  );
}
