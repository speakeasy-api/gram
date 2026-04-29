import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { TextArea } from "@/components/ui/textarea";
import { Type } from "@/components/ui/type";
import {
  invalidateAllNlPoliciesGet,
  invalidateAllNlPoliciesList,
  useNlPoliciesSetModeMutation,
  useNlPoliciesUpdateMutation,
} from "@gram/client/react-query/index.js";
import type { NLPolicy } from "@gram/client/models/components/nlpolicy.js";
import { useQueryClient } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { useState } from "react";

import NLPolicyModePromoteModal from "./NLPolicyModePromoteModal";
import NLPolicyReplayModal from "./NLPolicyReplayModal";

const TEMPLATES: { name: string; body: string }[] = [
  {
    name: "No deletes against prod",
    body: 'Refuse any tool call whose name or description indicates a destructive operation (delete, drop, truncate, purge) when the target MCP slug is tagged "production". Allow read operations.',
  },
  {
    name: "No PII egress",
    body: "Refuse any tool call that sends customer PII (email, SSN, phone, credit card) to an external destination such as Slack, email, or webhook.",
  },
  {
    name: "MCP allowlist",
    body: "Refuse any call to an external-MCP that is not on the configured allowlist.",
  },
  {
    name: "No secrets in args",
    body: "Refuse any tool call whose arguments contain values that look like API keys, passwords, or other credentials.",
  },
];

type FailMode = NLPolicy["failMode"];
type Mode = NLPolicy["mode"];

export default function NLPolicyConfigureTab({ policy }: { policy: NLPolicy }) {
  const queryClient = useQueryClient();
  const [name, setName] = useState(policy.name);
  const [description, setDescription] = useState(policy.description ?? "");
  const [nlPrompt, setNlPrompt] = useState(policy.nlPrompt);
  const [scopePerCall, setScopePerCall] = useState(policy.scopePerCall);
  const [scopeSession, setScopeSession] = useState(policy.scopeSession);
  const [failMode, setFailMode] = useState<FailMode>(policy.failMode);
  const [replayOpen, setReplayOpen] = useState(false);
  const [promoteOpen, setPromoteOpen] = useState(false);

  const invalidate = () => {
    invalidateAllNlPoliciesGet(queryClient);
    invalidateAllNlPoliciesList(queryClient);
  };

  const updateMutation = useNlPoliciesUpdateMutation({
    onSuccess: invalidate,
  });
  const setModeMutation = useNlPoliciesSetModeMutation({
    onSuccess: invalidate,
  });

  const onSave = () => {
    updateMutation.mutate({
      request: {
        updatePolicyRequestBody: {
          policyId: policy.id,
          name,
          description,
          nlPrompt,
          scopePerCall,
          scopeSession,
          failMode,
        },
      },
    });
  };

  const onPickTemplate = (idx: number) => {
    const template = TEMPLATES[idx];
    if (template) setNlPrompt(template.body);
  };

  const onPromoteConfirm = (mode: Mode) => {
    setModeMutation.mutate({
      request: {
        setModeRequestBody: {
          policyId: policy.id,
          mode,
        },
      },
    });
  };

  const isSaving = updateMutation.isPending;

  return (
    <Card className="space-y-6 p-6">
      <div className="space-y-2">
        <Label>Name</Label>
        <Input value={name} onChange={(v) => setName(v)} />
      </div>

      <div className="space-y-2">
        <Label>Description</Label>
        <Input value={description} onChange={(v) => setDescription(v)} />
      </div>

      <div className="space-y-2">
        <Label>Policy prompt</Label>
        <TextArea
          rows={8}
          value={nlPrompt}
          onChange={(v) => setNlPrompt(v)}
          className="font-mono text-sm"
        />
        <div className="mt-2 flex items-center gap-2">
          <Label className="text-xs font-normal">Use template:</Label>
          <select
            className="border-input bg-background h-8 rounded-md border px-2 text-sm"
            onChange={(e) => {
              const v = parseInt(e.target.value, 10);
              if (!Number.isNaN(v)) onPickTemplate(v);
              e.currentTarget.selectedIndex = 0;
            }}
            defaultValue=""
          >
            <option value="" disabled>
              Pick a template…
            </option>
            {TEMPLATES.map((t, i) => (
              <option key={t.name} value={i}>
                {t.name}
              </option>
            ))}
          </select>
        </div>
      </div>

      <div className="space-y-2">
        <Label>Scope</Label>
        <div className="space-y-2">
          <label className="flex items-start gap-2 text-sm">
            <Checkbox
              checked={scopePerCall}
              onCheckedChange={(v) => setScopePerCall(v === true)}
              className="mt-0.5"
            />
            <div>
              <div className="font-medium">Per tool call</div>
              <Type small muted>
                Synchronous, blocks before execution.
              </Type>
            </div>
          </label>
          <label className="flex items-start gap-2 text-sm">
            <Checkbox
              checked={scopeSession}
              onCheckedChange={(v) => setScopeSession(v === true)}
              className="mt-0.5"
            />
            <div>
              <div className="font-medium">Per session</div>
              <Type small muted>
                Async, can quarantine the session for future calls.
              </Type>
            </div>
          </label>
        </div>
      </div>

      <div className="space-y-2">
        <Label>Mode</Label>
        <Type small muted>
          Currently: <strong className="text-foreground">{policy.mode}</strong>
        </Type>
        <div>
          <Button
            variant="outline"
            size="sm"
            onClick={() => setPromoteOpen(true)}
          >
            Change mode…
          </Button>
        </div>
      </div>

      <div className="space-y-2">
        <Label>Failure behavior</Label>
        <RadioGroup
          value={failMode}
          onValueChange={(v) => setFailMode(v as FailMode)}
        >
          <label className="flex items-start gap-2 text-sm">
            <RadioGroupItem value="fail_open" className="mt-0.5" />
            <div>
              <div className="font-medium">Fail open</div>
              <Type small muted>
                Allow the call when the judge errors; record JUDGE_ERROR.
              </Type>
            </div>
          </label>
          <label className="flex items-start gap-2 text-sm">
            <RadioGroupItem value="fail_closed" className="mt-0.5" />
            <div>
              <div className="font-medium">Fail closed</div>
              <Type small muted>
                Block the call when the judge errors.
              </Type>
            </div>
          </label>
        </RadioGroup>
      </div>

      <div className="flex flex-wrap gap-3 pt-2">
        <Button variant="outline" onClick={() => setReplayOpen(true)}>
          Run replay against last 7d
        </Button>
        <Button onClick={onSave} disabled={isSaving}>
          {isSaving && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
          Save
        </Button>
      </div>

      {replayOpen && (
        <NLPolicyReplayModal
          policy={policy}
          onClose={() => setReplayOpen(false)}
        />
      )}
      {promoteOpen && (
        <NLPolicyModePromoteModal
          policy={policy}
          onClose={() => setPromoteOpen(false)}
          onConfirm={onPromoteConfirm}
        />
      )}
    </Card>
  );
}
