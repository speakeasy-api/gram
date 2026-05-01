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
  const [scopePerCall, setScopePerCall] = useState(true);
  const [scopeSession, setScopeSession] = useState(false);

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
    setScopePerCall(true);
    setScopeSession(false);
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
          scopePerCall,
          scopeSession,
        },
      },
    });
  };

  return (
    <Sheet open={open} onOpenChange={handleOpenChange}>
      <SheetContent className="flex flex-col overflow-y-auto sm:max-w-lg">
        <SheetHeader>
          <SheetTitle>New NL Policy</SheetTitle>
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
              placeholder="Refuse any tool call that…"
              className="font-mono text-sm"
            />
          </div>
          <div className="space-y-2">
            <Label>Scope</Label>
            <label className="flex items-center gap-2 text-sm">
              <Checkbox
                checked={scopePerCall}
                onCheckedChange={(v) => setScopePerCall(v === true)}
              />
              Per tool call
            </label>
            <label className="flex items-center gap-2 text-sm">
              <Checkbox
                checked={scopeSession}
                onCheckedChange={(v) => setScopeSession(v === true)}
              />
              Per session
            </label>
          </div>
        </div>
        <SheetFooter>
          <Button
            onClick={onSubmit}
            disabled={!name.trim() || !nlPrompt.trim() || create.isPending}
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
