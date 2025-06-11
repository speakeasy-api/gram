import { Page } from "@/components/page-layout";
import {
  invalidateTemplate,
  useTemplate,
  useUpdateTemplateMutation,
} from "@gram/client/react-query/index.js";
import { useQueryClient } from "@tanstack/react-query";
import { useParams } from "react-router";
import { PromptEditor } from "./PromptEditor";

export default function PromptPage() {
  const { promptName } = useParams();
  const { data, error, status } = useTemplate({ name: promptName });
  const queryClient = useQueryClient();
  const m = useUpdateTemplateMutation({
    onSettled: () => {
      invalidateTemplate(queryClient, [{ name: promptName }]);
    },
  });

  let content = null;
  if (status === "pending") {
    content = null;
  } else if (status === "error") {
    content = <p className="text-red-500">{error.message}</p>;
  } else if (status === "success" && data) {
    content = (
      <PromptEditor
        predecessor={data.template}
        status={m.status}
        error={m.error}
        handleSubmit={(form) => {
          m.mutate({
            request: {
              updatePromptTemplateForm: {
                id: form.id,
                prompt: form.prompt,
                description: form.description ?? void 0,
                arguments: form.arguments,
              },
            },
          });
        }}
      />
    );
  } else {
    content = (
      <p className="text-red-500">No data returned for prompt template</p>
    );
  }

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>{content}</Page.Body>
    </Page>
  );
}
