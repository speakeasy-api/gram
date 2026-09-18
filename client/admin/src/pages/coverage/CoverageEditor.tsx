import { useState, type JSX } from "react";
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

export function Choice({
  label,
  value,
  options,
  onChange,
}: {
  label: string;
  value: string;
  options: { value: string; label: string }[];
  onChange: (value: string) => void;
}): JSX.Element {
  return (
    <Select value={value} onValueChange={onChange}>
      <SelectTrigger aria-label={label} className="w-full">
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

export function FactEditor({
  fact,
  onChange,
}: {
  fact: Fact;
  onChange: (fact: Fact) => void;
}): JSX.Element {
  return (
    <div className="space-y-3">
      <Choice
        label="Coverage status"
        value={fact.status}
        options={Object.entries(statusLabels).map(([value, label]) => ({
          value,
          label,
        }))}
        onChange={(status) =>
          onChange({ ...fact, status: status as Fact["status"] })
        }
      />
      <Input
        aria-label="Coverage limitations"
        placeholder="Limitations, mechanism, or evidence…"
        value={fact.note}
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
}: {
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
    <section className="space-y-4 rounded-lg border p-4">
      <div>
        <h3 className="font-medium">{method.name}</h3>
        <p className="text-muted-foreground mt-1 text-xs">{method.plans}</p>
      </div>
      {product && (
        <>
          <Choice
            label={`Applicability of ${method.name}`}
            value={mapping.applicability}
            options={[
              { value: "unknown", label: "Applicability unknown" },
              { value: "applicable", label: "Applies to this product" },
              { value: "na", label: "Does not apply" },
            ]}
            onChange={(value) =>
              updateMapping({
                ...mapping,
                applicability: value as Mapping["applicability"],
              })
            }
          />
          <Input
            aria-label="OS and plan conditions"
            placeholder="OS / plan conditions, e.g. macOS, Enterprise only"
            value={mapping.conditions}
            onChange={(event) =>
              updateMapping({ ...mapping, conditions: event.target.value })
            }
          />
        </>
      )}
      {(!product || mapping.applicability === "applicable") && (
        <FactEditor
          fact={fact}
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

export function MethodSummary({
  method,
  product,
  capability,
  draft,
}: {
  method: Method;
  product: Product;
  capability: Capability;
  draft: Draft;
}): JSX.Element {
  const mapping =
    draft.mappings[mappingKey(method.id, product.id)] ?? emptyMapping;
  const fact = getFact(
    mapping,
    capability.id,
    methodReference(draft, method, capability.id),
  );
  return (
    <span className="flex w-full items-center justify-between gap-3">
      <span>{method.name}</span>
      <span className="text-muted-foreground text-xs">
        {symbols[fact.status]} {statusLabels[fact.status]}
      </span>
    </span>
  );
}
