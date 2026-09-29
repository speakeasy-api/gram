import { Field, FieldError, FieldLabel } from "@/components/ui/Field";
import { Input } from "@/components/ui/Input";
import { VerifyRemoteMcpUrlAlert } from "@/pages/sources/remote-mcp/VerifyRemoteMcpUrlButton";
import type { UpstreamUrlDraft } from "./useUpstreamUrlDraft";

export function UpstreamUrlField({
  upstream,
}: {
  upstream: UpstreamUrlDraft;
}): JSX.Element {
  return (
    <Field
      data-invalid={upstream.fieldError ? true : undefined}
      className="max-w-md"
    >
      <FieldLabel htmlFor="mcp-upstream-url">Remote URL</FieldLabel>
      <Input
        id="mcp-upstream-url"
        value={upstream.draft}
        onChange={upstream.setDraft}
        onBlur={upstream.touch}
        placeholder="https://example.com/mcp"
        disabled={upstream.pending}
        aria-invalid={upstream.fieldError ? true : undefined}
      />
      {upstream.fieldError && <FieldError>{upstream.fieldError}</FieldError>}
      <VerifyRemoteMcpUrlAlert state={upstream.verify} />
    </Field>
  );
}
