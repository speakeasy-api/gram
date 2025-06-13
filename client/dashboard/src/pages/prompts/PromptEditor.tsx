import { InputField } from "@/components/moon/input-field";
import { Textarea } from "@/components/moon/textarea";
import { Button } from "@/components/ui/button";
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
    <div>
      <form
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
        <div className="mb-6 grid grid-cols-1 gap-4 content-start">
          {predecessor == null ? (
            <InputField
              label="Name"
              name="name"
              pattern={PROMPT_NAME_PATTERN}
              required
            />
          ) : null}
          <div>
            <InputField
              label="Description"
              name="description"
              defaultValue={predecessor?.description ?? ""}
            />
          </div>
          <div className="grid lg:grid-cols-2 gap-4">
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
                <Label className="mb-2" htmlFor="newprompt_prompt">
                  Prompt
                </Label>
                <Textarea
                  id="newprompt_prompt"
                  name="prompt"
                  rows={fullScreenEditor ? void 0 : 20}
                  className={cn(
                    "font-mono",
                    fullScreenEditor ? "h-full" : false
                  )}
                  required
                  defaultValue={predecessor?.prompt}
                  onKeyUp={handleKeyUp}
                />
                {fullScreenEditor ? (
                  <Button
                    className="absolute top-4 right-4"
                    type="button"
                    variant="ghost"
                    onClick={() => setFullScreenEditor(false)}
                  >
                    <span className="sr-only">Exit full screen</span>
                    <X className="w-4 h-4" />
                  </Button>
                ) : null}
                {!fullScreenEditor ? (
                  <Button
                    className="absolute bottom-4 right-4"
                    type="button"
                    variant="ghost"
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
            <div>
              <fieldset>
                <legend className="text-sm font-medium leading-none">
                  Arguments
                </legend>
                <p className="text-muted-foreground mb-4">
                  Add useful descriptions for your prompt template arguments
                  (optional)
                </p>
                {args.length > 0 ? (
                  <ul>
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
                  <p>
                    No arguments found in prompt template. You can add these
                    using the syntax{" "}
                    <code className="text-red-600">{"{{argument_name}}"}</code>.
                  </p>
                )}
              </fieldset>
            </div>
          </div>
        </div>
        {error ? <p className="text-red-600 mb-4">{error.message}</p> : null}
        <Button type="submit" disabled={isPending}>
          {isPending ? <Loader2 className="w-4 h-4 mr-2 animate-spin" /> : null}
          {isPending ? "Saving..." : "Save Prompt"}
        </Button>
      </form>
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
