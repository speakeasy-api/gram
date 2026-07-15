import { Alert } from "@/components/ui/alert";
import { Field, FieldLabel } from "@/components/ui/field";
import { Heading } from "@/components/ui/heading";
import { InlineEmptyState } from "@/components/ui/inline-empty-state";
import { Label } from "@/components/ui/label";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { MUSTACHE_VAR_REGEX, PROMPT_NAME_PATTERN } from "@/lib/constants";
import { assert, cn } from "@/lib/utils";
import { PromptTemplate } from "@gram/client/models/components/prompttemplate.js";
import { MutationStatus } from "@tanstack/react-query";
import { Fullscreen, Loader2, X } from "lucide-react";
import { useCallback, useId, useState } from "react";
import * as z from "zod";

const argsSchema = z.object({
  properties: z.record(
    z.string(),
    z.object({
      description: z.string().optional(),
    }),
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
)): JSX.Element {
  const isPending = status === "pending";
  const nameFieldId = useId();
  const descriptionFieldId = useId();
  const [fullScreenEditor, setFullScreenEditor] = useState(false);
  const parsedArgs = argsSchema.safeParse(
    JSON.parse(predecessor?.schema || "{}"),
  );
  const argDefaults = parsedArgs.success ? parsedArgs.data.properties : {};
  const [args, setArgs] = useState<string[]>(Object.keys(argDefaults));

  const handleKeyUp = useCallback(
    (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
      const el = e.currentTarget;
      assert(
        el instanceof HTMLTextAreaElement,
        "Event target is not a textarea",
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
    [],
  );

  return (
    <div className="flex gap-8">
      <form
        className="min-w-0 flex-1 space-y-8"
        onSubmit={(e) => {
          e.preventDefault();
          const fd = new FormData(e.currentTarget);
          const name = fd.get("name");
          const description = fd.get("description");
          const prompt = fd.get("prompt");

          assert(typeof prompt === "string", "Prompt is required");
          assert(
            description == null || typeof description === "string",
            "Description is required",
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
              <Field>
                <FieldLabel htmlFor={nameFieldId}>Name</FieldLabel>
                <Input
                  id={nameFieldId}
                  name="name"
                  pattern={PROMPT_NAME_PATTERN}
                  placeholder="my-prompt-name"
                  title="Only lowercase letters, numbers, hyphens, and underscores (max 128 characters)"
                  required
                />
              </Field>
            </div>
          ) : null}
          <div className="max-w-md">
            <Field>
              <FieldLabel htmlFor={descriptionFieldId} optional>
                Description
              </FieldLabel>
              <Input
                id={descriptionFieldId}
                name="description"
                defaultValue={predecessor?.description ?? ""}
              />
            </Field>
          </div>
          <div>
            <dialog
              open
              className="bg-background fixed inset-0 z-50 h-dvh w-dvw text-inherit"
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
                    ? "flex h-full w-full flex-col px-8 pt-12 pb-8"
                    : false,
                )}
              >
                <Label className="mb-3" htmlFor="newprompt_prompt">
                  Prompt
                </Label>
                {/*
                  A plain native textarea, not the shared TextArea/Input
                  primitives: this editor needs onKeyUp (to detect mustache
                  variables as the user types) and a monospace/full-height
                  style swap for the fullscreen mode, neither of which the
                  shared components' prop surfaces support.
                */}
                <textarea
                  id="newprompt_prompt"
                  name="prompt"
                  rows={fullScreenEditor ? undefined : 4}
                  className={cn(
                    "border-input placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-ring/50 aria-invalid:ring-destructive/20 dark:aria-invalid:ring-destructive/40 aria-invalid:border-destructive dark:bg-input/30 flex field-sizing-content min-h-16 w-full border bg-transparent px-3 py-2 text-base transition-[color,box-shadow] outline-none focus-visible:ring-[3px] disabled:cursor-not-allowed disabled:opacity-50 md:text-sm",
                    "font-mono",
                    fullScreenEditor ? "h-full" : "",
                  )}
                  required
                  defaultValue={predecessor?.prompt}
                  onKeyUp={handleKeyUp}
                />
                {fullScreenEditor ? (
                  <Button
                    className="absolute top-4 right-4"
                    type="button"
                    variant="tertiary"
                    onClick={() => setFullScreenEditor(false)}
                  >
                    <span className="sr-only">Exit full screen</span>
                    <X className="h-4 w-4" />
                  </Button>
                ) : null}
                {!fullScreenEditor ? (
                  <Button
                    className="absolute right-4 bottom-4"
                    type="button"
                    variant="tertiary"
                    onClick={() => {
                      setFullScreenEditor(true);
                      document.querySelector("textarea")?.focus();
                    }}
                  >
                    <span className="sr-only">Enter full screen</span>
                    <Fullscreen className="h-4 w-4" />
                  </Button>
                ) : null}
              </div>
            </dialog>
          </div>
          <div className="pt-4">
            <fieldset className="space-y-4">
              <legend className="mb-3 text-base leading-none font-medium">
                Arguments
              </legend>
              <p className="text-muted-foreground mb-4 text-sm">
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
                <InlineEmptyState
                  title="No arguments found in prompt template"
                  description={
                    <>
                      You can add these using the syntax{" "}
                      <code className="bg-muted rounded px-1 py-0.5 text-xs">
                        {"{{argument_name}}"}
                      </code>
                      .
                    </>
                  }
                />
              )}
            </fieldset>
          </div>
        </div>
        <div className="pt-6">
          {error ? (
            <Alert variant="error" dismissible={false} className="mb-4">
              {error.message}
            </Alert>
          ) : null}
          <Button type="submit" disabled={isPending} size="md">
            {isPending ? (
              <Loader2 className="mr-2 h-4 w-4 animate-spin" />
            ) : null}
            {isPending ? "Saving..." : "Save Prompt"}
          </Button>
        </div>
      </form>
      <aside className="bg-secondary sticky top-8 w-80 flex-shrink-0 space-y-6 p-6">
        <div>
          <Heading variant="h6" className="mb-2 font-medium">
            Prompt Templates
          </Heading>
          <p className="text-muted-foreground text-sm">
            Create reusable prompts with dynamic variables using the Mustache
            syntax.
          </p>
        </div>
        <div>
          <Heading variant="h6" className="mb-2 font-medium">
            Using Variables
          </Heading>
          <p className="text-muted-foreground mb-2 text-sm">
            Add variables to your prompt using double curly braces:
          </p>
          <code className="bg-muted block rounded px-2 py-1 text-xs">
            {"{{variable_name}}"}
          </code>
        </div>
        <div>
          <Heading variant="h6" className="mb-2 font-medium">
            Arguments
          </Heading>
          <p className="text-muted-foreground text-sm">
            Variables detected in your prompt will automatically appear in the
            Arguments section. Add descriptions to help users understand what
            each variable is for.
          </p>
        </div>
        <div>
          <Heading variant="h6" className="mb-2 font-medium">
            Tips
          </Heading>
          <ul className="text-muted-foreground space-y-1 text-sm">
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
  const fieldId = useId();
  return (
    <li>
      <Field>
        <FieldLabel htmlFor={fieldId} optional>
          {name}
        </FieldLabel>
        <Input id={fieldId} name={`_${name}`} defaultValue={defaultValue} />
      </Field>
    </li>
  );
};
