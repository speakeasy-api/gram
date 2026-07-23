import { InputField } from "@/components/moon/input-field";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { Type } from "@/components/ui/type";
import { Button, Stack } from "@speakeasy-api/moonshine";
import { useState } from "react";
import { MOCK_PROJECTS } from "./data-events";
import {
  REDACTION_TARGETS,
  TARGET_DESCRIPTIONS,
  TARGET_LABELS,
  type RedactionRule,
  type RedactionTarget,
} from "./redaction-rules-data";

/** Sentinel value for the org-wide scope option in the project select. */
const ALL_PROJECTS = "__all__";

export function AddRedactionRuleSheet({
  open,
  onClose,
  onAdd,
}: {
  open: boolean;
  onClose: () => void;
  onAdd: (rule: RedactionRule) => void;
}): JSX.Element {
  const [name, setName] = useState("");
  const [email, setEmail] = useState("");
  const [targets, setTargets] = useState<RedactionTarget[]>([]);
  const [scope, setScope] = useState<string>(ALL_PROJECTS);

  const canSubmit = email.trim() !== "" && targets.length > 0;

  const reset = () => {
    setName("");
    setEmail("");
    setTargets([]);
    setScope(ALL_PROJECTS);
  };

  const handleClose = () => {
    reset();
    onClose();
  };

  const toggleTarget = (target: RedactionTarget) => {
    setTargets((prev) =>
      prev.includes(target)
        ? prev.filter((t) => t !== target)
        : [...prev, target],
    );
  };

  const handleSubmit = () => {
    if (!canSubmit) return;

    onAdd({
      id: crypto.randomUUID(),
      subjectName: name.trim(),
      subjectEmail: email.trim(),
      targets,
      project: scope === ALL_PROJECTS ? null : scope,
      enabled: true,
      createdAt: new Date(),
    });
    handleClose();
  };

  return (
    <Sheet
      open={open}
      onOpenChange={(isOpen) => {
        if (!isOpen) handleClose();
      }}
    >
      <SheetContent className="flex w-full flex-col sm:max-w-md">
        <SheetHeader>
          <SheetTitle>Add Redaction Rule</SheetTitle>
          <SheetDescription>
            Telemetry for this individual is redacted at ingest, before events
            are stored. Redaction is not retroactive.
          </SheetDescription>
        </SheetHeader>

        <Stack gap={5} className="flex-1 overflow-y-auto px-4">
          <InputField
            label="Name"
            placeholder="e.g., Ada Lovelace"
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
          <InputField
            label="Email"
            type="email"
            required
            placeholder="person@company.com"
            hint="Matched against identity attributes (user.email, session owner) on every incoming event."
            value={email}
            onChange={(e) => setEmail(e.target.value)}
          />

          <Stack gap={2}>
            <Type small className="font-medium">
              Redact
            </Type>
            {REDACTION_TARGETS.map((target) => (
              <label
                key={target}
                className="hover:bg-muted/50 flex cursor-pointer items-start gap-3 rounded-md border p-3"
              >
                <Checkbox
                  checked={targets.includes(target)}
                  onCheckedChange={() => toggleTarget(target)}
                  className="mt-0.5"
                />
                <div className="min-w-0">
                  <Type small className="block font-medium">
                    {TARGET_LABELS[target]}
                  </Type>
                  <Type muted small>
                    {TARGET_DESCRIPTIONS[target]}
                  </Type>
                </div>
              </label>
            ))}
          </Stack>

          <Stack gap={2}>
            <Type small className="font-medium">
              Applies to
            </Type>
            <Select value={scope} onValueChange={setScope}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={ALL_PROJECTS}>All projects</SelectItem>
                {MOCK_PROJECTS.map((project) => (
                  <SelectItem key={project} value={project}>
                    {project}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Stack>
        </Stack>

        <SheetFooter className="border-border flex-row justify-end border-t">
          <Button variant="secondary" onClick={handleClose}>
            <Button.Text>Cancel</Button.Text>
          </Button>
          <Button onClick={handleSubmit} disabled={!canSubmit}>
            <Button.Text>Add Rule</Button.Text>
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  );
}
