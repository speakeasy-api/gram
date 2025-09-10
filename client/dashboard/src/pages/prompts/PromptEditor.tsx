import { InputField } from "@/components/moon/input-field";
import { Textarea } from "@/components/moon/textarea";
import { Button } from "@speakeasy-api/moonshine";
import { Label } from "@/components/ui/label";
import { MUSTACHE_VAR_REGEX, PROMPT_NAME_PATTERN } from "@/lib/constants";
import { assert, cn } from "@/lib/utils";
import { PromptTemplate } from "@gram/client/models/components";
import { MutationStatus } from "@tanstack/react-query";
import { Fullscreen, Loader2, X } from "lucide-react";
import { useCallback, useState } from "react";
import * as z from "zod";

const argsSchema = z.object({
  properties: z.record(
    z.string(),
    z.object({
      description: z.string().optional(),
    })
  ),
});

export function PromptEditor({
  predecessor,
  error,
  status,
  handleSubmit,
}: {
  status: MutationStatus;
  error: Error | null;
} & (
  | {
      predecessor: PromptTemplate;
      handleSubmit: (form: {
        id: string;
        name: string;
        prompt: string;
        description?: string;
        arguments?: string;
      }) => void;
    }
  | {
      predecessor?: undefined;
      handleSubmit: (form: {
        name: string;
        prompt: string;
        description?: string;
        arguments?: string;
      }) => void;
    }
)) {
  const isPending = status === "pending";
  const [fullScreenEditor, setFullScreenEditor] = useState(false);
  const parsedArgs = argsSchema.safeParse(
    JSON.parse(predecessor?.arguments ?? "{}")
  );
  const argDefaults = parsedArgs.success ? parsedArgs.data.properties : {};
  const [args, setArgs] = useState<string[]>(Object.keys(argDefaults));

  const handleKeyUp = useCallback(
    (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
      const el = e.currentTarget;
      assert(
        el instanceof HTMLTextAreaElement,
        "Event target is not a textarea"
      );

      const iter = el.value.matchAll(MUSTACHE_VAR_REGEX);

      const names: string[] = [];
      for (const m of iter) {
        if (m[1]) {
          names.push(m[1]);
        }
      }

      setArgs([...new Set(names)].sort());
    },
    []
  );

  return (
    <div className="flex gap-8">
      <form
        className="flex-1 min-w-0 space-y-8"
        onSubmit={(e) => {
          e.preventDefault();
          const fd = new FormData(e.currentTarget);
          const name = fd.get("name");
          const description = fd.get("description");
          const prompt = fd.get("prompt");

          assert(typeof prompt === "string", "Prompt is required");
          assert(
            description == null || typeof description === "string",
            "Description is required"
          );

          let hasArgs = false;
          const properties: Record<
            string,
            { type: "string"; description?: string }
          > = {};
          for (const name of args) {
            const value = fd.get(`_${name}`);
            properties[name] = {
              type: "string",
              description: typeof value === "string" ? value : void 0,
            };
            hasArgs = true;
          }

          const f = {
            prompt: prompt,
            description: description ?? void 0,
            arguments: hasArgs
              ? JSON.stringify({
                  type: "object",
                  properties,
                })
              : void 0,
          };

          if (predecessor) {
            handleSubmit({ ...f, id: predecessor.id, name: predecessor.name });
          } else {
            assert(typeof name === "string", "Name is required");
            handleSubmit({ ...f, name });
          }
        }}
      >
        <div className="space-y-6">
          {predecessor == null ? (
            <div className="max-w-md">
              <InputField
                label="Name"
                name="name"
                pattern={PROMPT_NAME_PATTERN}
                required
              />
            </div>
          ) : null}
          <div className="max-w-md">
            <InputField
              label="Description"
              name="description"
              defaultValue={predecessor?.description ?? ""}
            />
          </div>
          <div>
            <dialog
              open
              className="bg-background text-inherit w-dvw h-dvh fixed inset-0 z-50"
              style={{ all: fullScreenEditor ? void 0 : "unset" }}
              onKeyDown={(e) => {
                if (e.key === "Escape") {
                  setFullScreenEditor(false);
                }
              }}
            >
              <div
                className={cn(
                  "relative",
                  fullScreenEditor
                    ? "px-8 pb-8 pt-12 h-full w-full flex flex-col"
                    : false
                )}
              >
                <Label className="mb-3" htmlFor="newprompt_prompt">
                  Prompt
                </Label>
                <Textarea
                  id="newprompt_prompt"
                  name="prompt"
                  rows={fullScreenEditor ? void 0 : 4}
                  className={cn("font-mono", fullScreenEditor ? "h-full" : "")}
                  required
                  defaultValue={predecessor?.prompt}
                  onKeyUp={handleKeyUp}
                />
                {fullScreenEditor ? (
                  <Button className="absolute top-4 right-4"
                    type="button"
                    variant="tertiary"
                    onClick={() => setFullScreenEditor(false)}
                  >
                    <span className="sr-only">Exit full screen</span>
                    <X className="w-4 h-4" />
                  </Button>
                ) : null}
                {!fullScreenEditor ? (
                  <Button className="absolute bottom-4 right-4"
                    type="button"
                    variant="tertiary"
                    onClick={() => {
                      setFullScreenEditor(true);
                      document.querySelector("textarea")?.focus();
                    }}
                  >
                    <span className="sr-only">Enter full screen</span>
                    <Fullscreen className="w-4 h-4" />
                  </Button>
                ) : null}
              </div>
            </dialog>
          </div>
          <div className="pt-4">
            <fieldset className="space-y-4">
              <legend className="text-base font-medium leading-none mb-3">
                Arguments
              </legend>
              <p className="text-muted-foreground text-sm mb-4">
                Add useful descriptions for your prompt template arguments
                (optional)
              </p>
              {args.length > 0 ? (
                <ul className="space-y-3">
                  {args.map((name) => {
                    return (
                      <ArgumentEntry
                        key={name}
                        name={name}
                        defaultValue={argDefaults[name]?.description}
                      />
                    );
                  })}
                </ul>
              ) : (
                <p className="text-sm text-muted-foreground border border-dashed border-muted-foreground/20 rounded-md p-4">
                  No arguments found in prompt template. You can add these using
                  the syntax{" "}
                  <code className="text-red-600 bg-red-50 px-1 py-0.5 rounded text-xs">
                    {"{{argument_name}}"}
                  </code>
                  .
                </p>
              )}
            </fieldset>
          </div>
        </div>
        <div className="pt-6">
          {error ? (
            <div className="bg-red-50 border border-red-200 rounded-md p-3 mb-4">
              <p className="text-red-700 text-sm">{error.message}</p>
            </div>
          ) : null}
          <Button type="submit" disabled={isPending} size="md">
            {isPending ? (
              <Loader2 className="w-4 h-4 mr-2 animate-spin" />
            ) : null}
            {isPending ? "Saving..." : "Save Prompt"}
          </Button>
        </div>
      </form>
      <aside className="w-80 flex-shrink-0 space-y-6 sticky top-8 bg-secondary p-6 rounded-lg">
        <div>
          <h3 className="text-sm font-medium mb-2">Prompt Templates</h3>
          <p className="text-sm text-muted-foreground">
            Create reusable prompts with dynamic variables using the Mustache
            syntax.
          </p>
        </div>
        <div>
          <h3 className="text-sm font-medium mb-2">Using Variables</h3>
          <p className="text-sm text-muted-foreground mb-2">
            Add variables to your prompt using double curly braces:
          </p>
          <code className="text-xs bg-muted px-2 py-1 rounded block">
            {"{{variable_name}}"}
          </code>
        </div>
        <div>
          <h3 className="text-sm font-medium mb-2">Arguments</h3>
          <p className="text-sm text-muted-foreground">
            Variables detected in your prompt will automatically appear in the
            Arguments section. Add descriptions to help users understand what
            each variable is for.
          </p>
        </div>
        <div>
          <h3 className="text-sm font-medium mb-2">Tips</h3>
          <ul className="text-sm text-muted-foreground space-y-1">
            <li>• Use descriptive variable names</li>
            <li>• Keep prompts clear and concise</li>
            <li>• Test with different inputs</li>
            <li>• Use the fullscreen editor for long prompts</li>
          </ul>
        </div>
      </aside>
    </div>
  );
}

const ArgumentEntry = ({
  name,
  defaultValue,
}: {
  name: string;
  defaultValue?: string;
}) => {
  return (
    <li>
      <InputField label={name} name={`_${name}`} defaultValue={defaultValue} />
    </li>
  );
};
