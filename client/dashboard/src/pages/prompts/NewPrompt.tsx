import { Page } from "@/components/page-layout";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { TextArea } from "@/components/ui/textarea";
import { assert } from "@/lib/utils";
import { useRoutes } from "@/routes";
import {
  invalidateAllTemplates,
  useCreateTemplateMutation,
} from "@gram/client/react-query";
import { useQueryClient } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";

export default function NewPromptPage() {
  const queryClient = useQueryClient();
  const routes = useRoutes();
  const {
    mutate: createPrompt,
    isPending,
    error,
  } = useCreateTemplateMutation({
    onSuccess: () => {
      routes.prompts.goTo();
    },
    onSettled: () => {
      invalidateAllTemplates(queryClient);
    },
  });

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <form
          onSubmit={(e) => {
            e.preventDefault();
            const fd = new FormData(e.currentTarget);
            const name = fd.get("name");
            const description = fd.get("description");
            const prompt = fd.get("prompt");

            assert(typeof name === "string", "Name is required");
            assert(typeof prompt === "string", "Prompt is required");
            assert(
              description == null || typeof description === "string",
              "Prompt is required"
            );

            if (name && prompt) {
              createPrompt({
                request: {
                  createPromptTemplateForm: {
                    engine: "mustache",
                    kind: "prompt",
                    name: name,
                    prompt: prompt,
                    description: description ?? void 0,
                  },
                },
              });
            }
          }}
        >
          <div className="mb-6 grid grid-cols-1 gap-4 content-start">
            <div>
              <Label className="mb-2" htmlFor="newprompt_name">
                Name
              </Label>
              <Input id="newprompt_name" name="name" required />
            </div>
            <div>
              <Label className="mb-2" htmlFor="newprompt_description">
                Description
              </Label>
              <Input id="newprompt_description" name="description" />
            </div>
            <div>
              <Label className="mb-2" htmlFor="newprompt_prompt">
                Prompt
              </Label>
              <TextArea
                id="newprompt_prompt"
                name="prompt"
                rows={20}
                className="font-mono"
                required
              />
            </div>
          </div>
          {error ? <p className="text-red-600 mb-4">{error.message}</p> : null}
          <Button type="submit" disabled={isPending}>
            {isPending ? (
              <Loader2 className="w-4 h-4 mr-2 animate-spin" />
            ) : null}
            {isPending ? "Creating..." : "Create Prompt"}
          </Button>
        </form>
      </Page.Body>
    </Page>
  );
}
