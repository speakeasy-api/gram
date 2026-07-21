import { Type } from "@/components/ui/type";
import { ProxyRegistrationError } from "@/lib/proxyRegisterUpstreamClient";
import { Alert, Stack } from "@speakeasy-api/moonshine";

type ErrorContent = {
  title: string;
  message: string;
};

function errorContent(error: unknown): ErrorContent | null {
  if (!error) {
    return null;
  }

  if (error instanceof ProxyRegistrationError) {
    return { title: error.title, message: error.message };
  }

  return {
    title: "Failed to attach identity provider",
    message:
      error instanceof Error && error.message
        ? error.message
        : "An unexpected error occurred. Please try again.",
  };
}

export function IdentityProviderAttachmentErrorAlert({
  error,
}: {
  error: unknown;
}): JSX.Element | null {
  const content = errorContent(error);
  if (!content) {
    return null;
  }

  return (
    <Alert variant="error" dismissible={false}>
      <Stack gap={1}>
        <Type className="font-medium">{content.title}</Type>
        <Type small>{content.message}</Type>
      </Stack>
    </Alert>
  );
}
