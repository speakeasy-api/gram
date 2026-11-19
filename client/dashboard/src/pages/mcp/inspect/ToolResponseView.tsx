import { CodeSnippet } from "@/components/ui/CodeSnippet";
import { Dialog } from "@/components/ui/Dialog";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import { Markdown } from "@/elements/components/Markdown";
import { remarkDropImages } from "@/lib/remarkDropImages";
import { cn } from "@/lib/utils";
import { Maximize2 } from "lucide-react";
import { type ComponentPropsWithoutRef, useState } from "react";

type View = "text" | "json";

const VIEW_KEY = "inspect.response-view";
const REMARK_PLUGINS = [remarkDropImages];
const PROSE =
  "text-sm leading-6 break-words [&_a]:underline [&_a]:underline-offset-2 [&_code]:font-mono [&_code]:text-[12px] [&_h1]:mt-4 [&_h1]:text-base [&_h1]:font-semibold [&_h2]:mt-4 [&_h2]:text-sm [&_h2]:font-semibold [&_h3]:mt-3 [&_h3]:font-medium [&_li]:my-1 [&_ol]:ml-6 [&_ol]:list-decimal [&_p]:my-2 [&_pre]:my-2 [&_pre]:overflow-x-auto [&_pre]:bg-muted/40 [&_pre]:p-3 [&_strong]:font-semibold [&_ul]:ml-6 [&_ul]:list-disc";

function readView(): View {
  try {
    return window.localStorage.getItem(VIEW_KEY) === "json" ? "json" : "text";
  } catch {
    return "text";
  }
}

function writeView(view: View): void {
  try {
    window.localStorage.setItem(VIEW_KEY, view);
  } catch {
    // A per-viewer convenience: without storage the choice just isn't kept.
  }
}

/** Links in a tool's text open away from the dashboard, never in its tab. */
function ExternalLink(props: ComponentPropsWithoutRef<"a">): JSX.Element {
  return <a {...props} target="_blank" rel="noopener noreferrer" />;
}

/**
 * A tool's result, readable by a person: the text the tool returned, rendered
 * as Markdown, or the JSON. One switch picks between them and the choice is
 * kept for next time; Expand opens the same view large, for long answers.
 * Images in the text are dropped, since the text comes from the server.
 */
export function ToolResponseView({
  text,
  json,
}: {
  /** The text content a client would show the model, joined. */
  text: string | undefined;
  /** The result as JSON, already formatted. */
  json: string | undefined;
}): JSX.Element | null {
  const [chosen, setChosen] = useState<View>(readView);
  const [expanded, setExpanded] = useState(false);
  const hasText = !!text?.trim();
  const view: View =
    hasText && json !== undefined ? chosen : hasText ? "text" : "json";
  if (!hasText && json === undefined) return null;

  const choose = (next: View) => {
    setChosen(next);
    writeView(next);
  };
  const body = (large: boolean) =>
    view === "text" ? (
      <div className={cn("px-4 py-3", PROSE)}>
        <Markdown
          extraRemarkPlugins={REMARK_PLUGINS}
          linkComponent={ExternalLink}
        >
          {text ?? ""}
        </Markdown>
      </div>
    ) : (
      <CodeSnippet
        language="json"
        code={json ?? ""}
        fontSize={large ? "medium" : "small"}
        wordWrap
        className="border-0"
      />
    );
  const toolbar = (
    <div className="border-border flex items-center justify-between gap-2 border-b px-3 py-1.5">
      {hasText && json !== undefined ? (
        <SegmentedControl
          ariaLabel="Show the response as"
          value={view}
          onChange={choose}
          options={[
            { value: "text", label: "Text" },
            { value: "json", label: "JSON" },
          ]}
          className="h-7"
        />
      ) : (
        <span className="text-muted-foreground text-xs">
          {view === "text" ? "Text" : "JSON"}
        </span>
      )}
      <button
        type="button"
        onClick={() => setExpanded(true)}
        aria-label="Expand the response"
        className="text-muted-foreground hover:text-foreground flex items-center gap-1 text-xs"
      >
        <Maximize2 className="size-3.5" />
        Expand
      </button>
    </div>
  );

  return (
    <>
      {toolbar}
      <div className="max-h-80 overflow-auto">{body(false)}</div>
      <Dialog open={expanded} onOpenChange={setExpanded}>
        <Dialog.Content className="flex max-h-[85vh] max-w-4xl flex-col">
          <Dialog.Header>
            <Dialog.Title>Response</Dialog.Title>
          </Dialog.Header>
          <div className="border-border flex min-h-0 flex-1 flex-col border">
            {hasText && json !== undefined ? (
              <div className="border-border border-b px-3 py-1.5">
                <SegmentedControl
                  ariaLabel="Show the response as"
                  value={view}
                  onChange={choose}
                  options={[
                    { value: "text", label: "Text" },
                    { value: "json", label: "JSON" },
                  ]}
                  className="h-7"
                />
              </div>
            ) : null}
            <div className="min-h-0 flex-1 overflow-auto">{body(true)}</div>
          </div>
        </Dialog.Content>
      </Dialog>
    </>
  );
}
