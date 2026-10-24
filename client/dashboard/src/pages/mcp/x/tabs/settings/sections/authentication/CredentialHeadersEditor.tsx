import { Button } from "@/components/ui/Button";
import { Checkbox } from "@/components/ui/Checkbox";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import { Text } from "@/components/ui/Text";
import { Lock, Plus, Trash2 } from "lucide-react";
import {
  generatesAuthorization,
  newHeaderDraft,
  type HeaderDraft,
} from "./credentialHeaders";

/**
 * The headers one credential sends upstream. A static credential sends stored
 * values and a caller-supplied header forwards the caller's, so each offers
 * only its own kind of row; a signed-in credential may add either beside the
 * Authorization header its sign-in produces.
 */
export function CredentialHeadersEditor({
  clientType,
  drafts,
  onChange,
  readOnly,
}: {
  clientType: string;
  drafts: HeaderDraft[];
  onChange: (drafts: HeaderDraft[]) => void;
  readOnly: boolean;
}): JSX.Element {
  const sources: HeaderDraft["source"][] =
    clientType === "static_credential"
      ? ["stored"]
      : clientType === "pass_through"
        ? ["forwarded"]
        : ["stored", "forwarded"];
  const update = (key: string, patch: Partial<HeaderDraft>) =>
    onChange(
      drafts.map((draft) =>
        draft.key === key ? { ...draft, ...patch } : draft,
      ),
    );

  return (
    <div className="space-y-2">
      <ul className="border-border divide-y border">
        {generatesAuthorization(clientType) ? (
          <li className="bg-muted/15 flex items-center gap-3 px-3 py-2">
            <Lock className="text-muted-foreground h-4 w-4 shrink-0" />
            <span className="text-sm font-medium">Authorization</span>
            <Text muted small className="ml-auto">
              Set by sign-in
            </Text>
          </li>
        ) : null}
        {drafts.map((draft) => (
          <li key={draft.key} className="space-y-2 px-3 py-3">
            <div className="flex items-center gap-2">
              <div className="min-w-0 flex-1">
                <Input
                  aria-label="Header name"
                  placeholder="Header name"
                  value={draft.name}
                  disabled={readOnly}
                  onChange={(name) => update(draft.key, { name })}
                />
              </div>
              {!readOnly ? (
                <Button
                  variant="tertiary"
                  size="sm"
                  aria-label={`Remove ${draft.name || "header"}`}
                  onClick={() =>
                    onChange(drafts.filter((d) => d.key !== draft.key))
                  }
                >
                  <Trash2 className="h-4 w-4" />
                </Button>
              ) : null}
            </div>
            {draft.source === "forwarded" ? (
              <Input
                aria-label={`Caller header for ${draft.name || "this header"}`}
                placeholder="Copied from the caller's header, e.g. X-Upstream-Token"
                value={draft.fromHeader}
                disabled={readOnly}
                onChange={(fromHeader) => update(draft.key, { fromHeader })}
              />
            ) : (
              <>
                <Input
                  aria-label={`Value for ${draft.name || "this header"}`}
                  placeholder={
                    draft.keepsStored
                      ? "Stored. Enter a new value to replace it."
                      : "Value"
                  }
                  value={draft.value}
                  disabled={readOnly}
                  reveal={draft.isSecret}
                  noAutofill
                  onChange={(value) => update(draft.key, { value })}
                />
                <div className="flex items-center gap-2">
                  <Checkbox
                    id={`${draft.key}-secret`}
                    checked={draft.isSecret}
                    disabled={readOnly || draft.keepsStored}
                    onCheckedChange={(checked) =>
                      update(draft.key, { isSecret: checked === true })
                    }
                  />
                  <Label htmlFor={`${draft.key}-secret`}>
                    <Text small>Secret: stored encrypted, never shown</Text>
                  </Label>
                </div>
              </>
            )}
          </li>
        ))}
      </ul>
      {!readOnly ? (
        <div className="flex gap-2">
          {sources.map((source) => (
            <Button
              key={source}
              variant="secondary"
              size="sm"
              onClick={() => onChange([...drafts, newHeaderDraft(source)])}
            >
              <Button.LeftIcon>
                <Plus className="h-4 w-4" />
              </Button.LeftIcon>
              <Button.Text>
                {source === "stored"
                  ? "Add stored header"
                  : "Add header from the caller"}
              </Button.Text>
            </Button>
          ))}
        </div>
      ) : null}
    </div>
  );
}
