import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Sheet,
  SheetContent,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { TextArea } from "@/components/ui/textarea";
import { useRoutes } from "@/routes";
import {
  invalidateAllNlPoliciesList,
  useNlPoliciesCreateMutation,
} from "@gram/client/react-query/index.js";
import { useQueryClient } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { useState } from "react";
import { CHECK_SCOPE_META, type CheckScope } from "./policy-data";

const DEFAULT_TARGETS: CheckScope[] = ["tool_arguments"];
const ALL_CHECK_SCOPES = Object.keys(CHECK_SCOPE_META) as CheckScope[];

export default function NLPolicyCreateForm({
  open,
  onClose,
}: {
  open: boolean;
  onClose: () => void;
}) {
  const queryClient = useQueryClient();
  const routes = useRoutes();
  const [name, setName] = useState("");
  const [nlPrompt, setNlPrompt] = useState("");
  const [targets, setTargets] = useState<CheckScope[]>(DEFAULT_TARGETS);

  const create = useNlPoliciesCreateMutation({
    onSuccess: (p) => {
      invalidateAllNlPoliciesList(queryClient);
      onClose();
      routes.policyCenter.nlDetail.goTo(p.id);
    },
  });

  const reset = () => {
    setName("");
    setNlPrompt("");
    setTargets(DEFAULT_TARGETS);
  };

  const handleOpenChange = (next: boolean) => {
    if (!next) {
      onClose();
      reset();
    }
  };

  const onSubmit = () => {
    create.mutate({
      request: {
        createPolicyRequestBody: {
          name,
          nlPrompt,
          targets,
        },
      },
    });
  };

  return (
    <Sheet open={open} onOpenChange={handleOpenChange}>
      <SheetContent className="flex flex-col overflow-y-auto sm:max-w-lg">
        <SheetHeader>
          <SheetTitle>New LLM Judge Policy</SheetTitle>
        </SheetHeader>
        <div className="flex-1 space-y-4 px-4">
          <div className="space-y-2">
            <Label>Name</Label>
            <Input
              value={name}
              onChange={(v) => setName(v)}
              placeholder="e.g. No deletes against prod"
            />
          </div>
          <div className="space-y-2">
            <Label>Policy prompt</Label>
            <TextArea
              rows={6}
              value={nlPrompt}
              onChange={(v) => setNlPrompt(v)}
              placeholder="Describe criteria to be matched by this policy"
              className="font-mono text-sm"
            />
          </div>
          <div className="space-y-2">
            <Label>Policy Scope</Label>
            {ALL_CHECK_SCOPES.map((target) => {
              const meta = CHECK_SCOPE_META[target];
              return (
                <label key={target} className="flex items-start gap-2 text-sm">
                  <Checkbox
                    checked={targets.includes(target)}
                    onCheckedChange={(checked) => {
                      const next = checked
                        ? targets.includes(target)
                          ? targets
                          : [...targets, target]
                        : targets.filter((value) => value !== target);
                      setTargets(next);
                    }}
                    className="mt-0.5"
                  />
                  <div>
                    <div className="font-medium">{meta.label}</div>
                    <div className="text-muted-foreground text-xs">
                      {meta.description}
                    </div>
                  </div>
                </label>
              );
            })}
          </div>
        </div>
        <SheetFooter>
          <Button
            onClick={onSubmit}
            disabled={
              !name.trim() ||
              !nlPrompt.trim() ||
              targets.length === 0 ||
              create.isPending
            }
          >
            {create.isPending && (
              <Loader2 className="mr-2 h-4 w-4 animate-spin" />
            )}
            Create
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  );
}
