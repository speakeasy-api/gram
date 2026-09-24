import { AnyField } from "@/components/moon/any-field";
import { InputField } from "@/components/moon/input-field";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import {
  Sheet,
  SheetContent,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/Sheet";
import { Text } from "@/components/ui/Text";
import { useOrganization } from "@/contexts/Auth";
import { assert } from "@/lib/utils";
import { Key } from "@gram/client/models/components/key.js";
import { useCreateAPIKeyMutation } from "@gram/client/react-query/createAPIKey";
import { invalidateListAPIKeys } from "@gram/client/react-query/listAPIKeys";
import { useQueryClient } from "@tanstack/react-query";
import { CheckCircle2, Copy } from "lucide-react";
import { useEffect, useState } from "react";
import { toast } from "sonner";
import { ApiKeyScopeField } from "./ApiKeyScopeField";
import {
  ORGANIZATION_WIDE,
  projectBindingLabel,
} from "./api-key-project-binding";

// CreateApiKeySheet runs the whole creation in one pane: the form, then the
// key itself. The secret is shown once, so the pane holds it rather than
// closing on success like the other creation sheets.
export function CreateApiKeySheet({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}): JSX.Element {
  const organization = useOrganization();
  const queryClient = useQueryClient();

  const [projectId, setProjectId] = useState(ORGANIZATION_WIDE);
  const [createdKey, setCreatedKey] = useState<Key | null>(null);
  const [isCopied, setIsCopied] = useState(false);

  const projectSelectionValid =
    projectId === ORGANIZATION_WIDE ||
    organization.projects.some((project) => project.id === projectId);

  const createKeyMutation = useCreateAPIKeyMutation({
    onSuccess: async (data) => {
      setCreatedKey(data);
      await invalidateListAPIKeys(queryClient, [{ gramSession: "" }]);
      await queryClient.refetchQueries({
        queryKey: ["@gram/client", "keys", "list"],
      });
    },
  });

  // Reset transient state whenever the sheet is reopened so a prior draft, or
  // the last key's secret, never leaks into a new creation.
  useEffect(() => {
    if (!open) return;
    setProjectId(ORGANIZATION_WIDE);
    setCreatedKey(null);
    setIsCopied(false);
  }, [open]);

  const handleCreateKey: React.FormEventHandler<HTMLFormElement> = (e) => {
    e.preventDefault();
    if (!projectSelectionValid || createKeyMutation.isPending) return;
    const formEl = e.currentTarget;
    const formData = new FormData(formEl);
    const newKeyName = formData.get("name");
    assert(typeof newKeyName === "string", "Key name must be a string");
    const scope = formData.get("scope");
    assert(typeof scope === "string", "Scope must be a string");

    createKeyMutation.mutate(
      {
        security: { sessionHeaderGramSession: "" },
        request: {
          createKeyForm: {
            name: newKeyName,
            projectId: projectId === ORGANIZATION_WIDE ? undefined : projectId,
            scopes: [scope],
          },
        },
      },
      {
        onSuccess: () => {
          formEl.reset();
        },
      },
    );
  };

  const handleCopyToken = async () => {
    if (!createdKey?.key) return;
    try {
      await navigator.clipboard.writeText(createdKey.key);
      setIsCopied(true);
      setTimeout(() => setIsCopied(false), 2000);
    } catch {
      // A denied or unavailable clipboard must not read as a successful copy:
      // this is the one moment the secret is recoverable, so say so and leave
      // it on screen to select by hand.
      setIsCopied(false);
      toast.error("Could not copy the key. Select it and copy it manually.");
    }
  };

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side="right"
        className="flex w-[560px] max-w-[calc(100vw-2rem)] flex-col sm:max-w-[560px]"
      >
        <SheetHeader className="px-6 pt-6 pb-0">
          <SheetTitle className="text-lg font-semibold">
            {createdKey ? "API Key Created" : "New API Key"}
          </SheetTitle>
        </SheetHeader>

        {createdKey ? (
          <>
            <div className="flex-1 space-y-6 overflow-y-auto px-6 py-6">
              <Alert variant="warning" alignTop>
                You will not be able to see this token value again once you
                close this panel. Copy it now and store it securely.
              </Alert>
              <Text variant="body">
                Project binding:{" "}
                {projectBindingLabel(
                  organization.projects,
                  createdKey.projectId,
                )}
              </Text>
              <div className="bg-muted flex items-center space-x-2 p-3">
                <code className="flex-1 break-all">{createdKey.key}</code>
                <Button
                  aria-label={isCopied ? "API key copied" : "Copy API key"}
                  variant="tertiary"
                  size="sm"
                  onClick={() => void handleCopyToken()}
                  className="shrink-0"
                >
                  <Button.Icon>
                    {isCopied ? (
                      <CheckCircle2 className="h-4 w-4 text-green-500" />
                    ) : (
                      <Copy className="h-4 w-4" />
                    )}
                  </Button.Icon>
                </Button>
              </div>
            </div>
            <SheetFooter className="flex-row items-center justify-end gap-2 border-t px-6 py-4">
              {/* Not "Close": the pane's own dismiss control already carries
                  that name, and two buttons with one name is ambiguous to
                  anyone navigating by accessible name. */}
              <Button variant="primary" onClick={() => onOpenChange(false)}>
                <Button.Text>Done</Button.Text>
              </Button>
            </SheetFooter>
          </>
        ) : (
          <form
            onSubmit={handleCreateKey}
            className="flex min-h-0 flex-1 flex-col"
          >
            <div className="flex-1 space-y-6 overflow-y-auto px-6 py-6">
              <InputField
                label="Key name"
                name="name"
                required
                autoFocus
                autoCapitalize="off"
                autoComplete="off"
                autoCorrect="off"
              />

              <AnyField
                label="Project"
                hint="Restrict this key to a project without changing its scope."
                error={
                  !projectSelectionValid &&
                  "This project is no longer available. Select a project or Organization-wide."
                }
                render={(props) => (
                  <Select value={projectId} onValueChange={setProjectId}>
                    <SelectTrigger
                      {...props}
                      aria-invalid={!projectSelectionValid}
                      className="w-full"
                    >
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value={ORGANIZATION_WIDE}>
                        Organization-wide
                      </SelectItem>
                      {organization.projects.map((project) => (
                        <SelectItem key={project.id} value={project.id}>
                          {project.name}
                        </SelectItem>
                      ))}
                      {!projectSelectionValid && (
                        <SelectItem value={projectId} disabled>
                          Unavailable project
                        </SelectItem>
                      )}
                    </SelectContent>
                  </Select>
                )}
              />

              <ApiKeyScopeField />
            </div>

            <SheetFooter className="flex-row items-center justify-end gap-2 border-t px-6 py-4">
              <Button
                type="button"
                variant="secondary"
                onClick={() => onOpenChange(false)}
              >
                <Button.Text>Cancel</Button.Text>
              </Button>
              <Button
                type="submit"
                variant="primary"
                disabled={createKeyMutation.isPending || !projectSelectionValid}
              >
                <Button.Text>
                  {createKeyMutation.isPending ? "Creating…" : "Create"}
                </Button.Text>
              </Button>
            </SheetFooter>
          </form>
        )}
      </SheetContent>
    </Sheet>
  );
}
