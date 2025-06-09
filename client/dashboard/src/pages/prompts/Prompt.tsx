import { Page } from "@/components/page-layout";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { TextArea } from "@/components/ui/textarea";
import { assert } from "@/lib/utils";
import { PromptTemplate } from "@gram/client/models/components";
import {
  invalidateTemplate,
  useTemplate,
  useUpdateTemplateMutation,
} from "@gram/client/react-query/index.js";
import { useQueryClient } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { useParams } from "react-router";

export default function PromptPage() {
  const { promptName } = useParams();
  const { data, error } = useTemplate({
    name: promptName,
  });

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        {error ? <p className="text-red-500">{error.message}</p> : null}
        {data ? (
          <PromptEditor template={data.template} />
        ) : (
          <p className="text-red-500">No data returned for prompt template</p>
        )}
      </Page.Body>
    </Page>
  );
}

function PromptEditor({ template }: { template: PromptTemplate }) {
  const queryClient = useQueryClient();
  const {
    mutate: updatePrompt,
    isPending,
    error,
  } = useUpdateTemplateMutation({
    onSettled: () => {
      invalidateTemplate(queryClient, [{ name: template.name }]);
    },
  });

  return (
    <div>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          const fd = new FormData(e.currentTarget);
          const description = fd.get("description");
          const prompt = fd.get("prompt");

          assert(typeof prompt === "string", "Prompt is required");
          assert(
            description == null || typeof description === "string",
            "Description is required"
          );

          updatePrompt({
            request: {
              updatePromptTemplateForm: {
                id: template.id,
                prompt: prompt,
                description: description ?? void 0,
              },
            },
          });
        }}
      >
        <div className="mb-6 grid grid-cols-1 gap-4 content-start">
          <div>
            <Label className="mb-2" htmlFor="newprompt_description">
              Description
            </Label>
            <Input
              id="newprompt_description"
              name="description"
              defaultValue={template.description ?? ""}
            />
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
              defaultValue={template.prompt}
            />
          </div>
        </div>
        {error ? <p className="text-red-600 mb-4">{error.message}</p> : null}
        <Button type="submit" disabled={isPending}>
          {isPending ? <Loader2 className="w-4 h-4 mr-2 animate-spin" /> : null}
          {isPending ? "Updating..." : "Update Prompt"}
        </Button>
      </form>
    </div>
  );
}
