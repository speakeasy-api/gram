import { Page } from "@/components/page-layout";
import { useRoutes } from "@/routes";
import { useCreateTemplateMutation } from "@gram/client/react-query/createTemplate.js";
import { invalidateAllTemplates } from "@gram/client/react-query/templates.js";
import { useQueryClient } from "@tanstack/react-query";
import { PromptEditor } from "./PromptEditor";

export default function NewPromptPage(): JSX.Element {
  const queryClient = useQueryClient();
  const routes = useRoutes();
  const {
    mutate: createPrompt,
    status,
    error,
  } = useCreateTemplateMutation({
    onSuccess: () => {
      routes.prompts.goTo();
    },
    onSettled: () => {
      void invalidateAllTemplates(queryClient);
    },
  });

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <PromptEditor
          predecessor={undefined}
          status={status}
          error={error}
          handleSubmit={(form) => {
            createPrompt({
              request: {
                createPromptTemplateForm: {
                  engine: "mustache",
                  kind: "prompt",
                  name: form.name,
                  prompt: form.prompt,
                  description: form.description ?? void 0,
                  arguments: form.arguments ?? void 0,
                },
              },
            });
          }}
        />
      </Page.Body>
    </Page>
  );
}
