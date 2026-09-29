import { Checkbox } from "@/components/ui/Checkbox";
import { Input } from "@/components/ui/Input";
import { RadioGroup, RadioGroupItem } from "@/components/ui/RadioGroup";
import { Text } from "@/components/ui/Text";
import { Loader2 } from "lucide-react";
import { useState, type Dispatch, type SetStateAction } from "react";
import { DetectorCard } from "../DetectorCard";
import { ActionStep } from "../PolicyDetail";
import type { PolicyAction, RuleCategory } from "../policy-data";
import { FLAG_ONLY_CATEGORIES, type DetectorMode } from "../policy-form";
import {
  destructiveToolNames,
  hasFlagOnlyCategory,
  SERVER_GUARDRAIL_CATEGORIES,
  type ServerGuardrailState,
  type ServerTool,
  type ServerToolMode,
} from "./server-guardrail-policy";

const NO_DISABLED_RULES: Set<string> = new Set();

export type ToolsSource =
  /** Tools are listable: show the all / selected picker. */
  | { status: "ready"; tools: ServerTool[] }
  | { status: "loading" }
  /** The server does not exist yet, so its tools cannot be listed. */
  | { status: "unavailable" };

function patchSetter<K extends keyof ServerGuardrailState>(
  key: K,
  onChange: (
    update: (state: ServerGuardrailState) => ServerGuardrailState,
  ) => void,
): Dispatch<SetStateAction<ServerGuardrailState[K]>> {
  return (value) =>
    onChange((state) => ({
      ...state,
      [key]:
        typeof value === "function"
          ? (
              value as (
                prev: ServerGuardrailState[K],
              ) => ServerGuardrailState[K]
            )(state[key])
          : value,
    }));
}

/**
 * The guardrail form shared by every place a server-scoped policy is authored:
 * Add Server, the catalog install, and the server's Guardrails tab. The server
 * (and so the scope) is fixed by the caller; tool requests and responses are
 * always inspected, so there is no message-type control.
 */
export function ServerGuardrailsForm({
  state,
  onChange,
  toolsSource,
  serverName,
  mode = "presidio",
}: {
  state: ServerGuardrailState;
  onChange: (
    update: (state: ServerGuardrailState) => ServerGuardrailState,
  ) => void;
  toolsSource: ToolsSource;
  serverName: string;
  mode?: DetectorMode;
}): JSX.Element {
  const toggleCategory = (category: RuleCategory, checked: boolean) =>
    onChange((prev) => {
      const categories = new Set(prev.categories);
      if (checked) categories.add(category);
      else categories.delete(category);
      const action: PolicyAction =
        checked && FLAG_ONLY_CATEGORIES.has(category) ? "flag" : prev.action;
      return { ...prev, categories, action };
    });

  return (
    <div className="space-y-6">
      <section className="space-y-3">
        <div>
          <h3 className="text-sm font-medium">Detect</h3>
          <Text small muted>
            What to look for in traffic through {serverName}.
          </Text>
        </div>
        <div className="grid gap-2">
          {SERVER_GUARDRAIL_CATEGORIES.map((category) => (
            <DetectorCard
              key={category}
              category={category}
              mode={mode}
              selected={state.categories.has(category)}
              disabledRules={NO_DISABLED_RULES}
              onToggle={(checked) => toggleCategory(category, checked)}
              onCustomize={() => undefined}
              hideCustomize
            />
          ))}
        </div>
      </section>

      <ToolsSection
        state={state}
        onChange={onChange}
        toolsSource={toolsSource}
      />

      <section className="space-y-1">
        <h3 className="text-sm font-medium">Inspects</h3>
        <Text small muted>
          Server guardrails always scan both directions of tool traffic. User
          prompts and assistant messages are covered by org-wide policies.
        </Text>
        <div className="border-border flex items-center justify-between border p-3">
          <div>
            <div className="text-sm font-medium">
              Tool requests and responses
            </div>
            <Text small muted>
              Arguments sent into tool calls and data returned from them
            </Text>
          </div>
          <span className="text-muted-foreground font-mono text-xs uppercase">
            Always on
          </span>
        </div>
      </section>

      <ActionStep
        action={state.action}
        setAction={patchSetter("action", onChange)}
        audienceType={state.audienceType}
        setAudienceType={patchSetter("audienceType", onChange)}
        audiencePrincipalUrns={state.audiencePrincipalUrns}
        setAudiencePrincipalUrns={patchSetter(
          "audiencePrincipalUrns",
          onChange,
        )}
        userMessage={state.userMessage}
        setUserMessage={patchSetter("userMessage", onChange)}
        score={state.score}
        setScore={patchSetter("score", onChange)}
        flagOnlySelected={hasFlagOnlyCategory(state.categories)}
      />
    </div>
  );
}

function ToolsSection({
  state,
  onChange,
  toolsSource,
}: {
  state: ServerGuardrailState;
  onChange: (
    update: (state: ServerGuardrailState) => ServerGuardrailState,
  ) => void;
  toolsSource: ToolsSource;
}): JSX.Element {
  if (toolsSource.status === "unavailable") {
    return (
      <section className="space-y-1">
        <h3 className="text-sm font-medium">Tools</h3>
        <Text small muted>
          The guardrail covers all of this server&apos;s tools, including ones
          added later. Narrow it to specific tools from the server&apos;s
          Guardrails tab once it is added.
        </Text>
      </section>
    );
  }

  const setToolMode = (toolMode: ServerToolMode) =>
    onChange((prev) => ({ ...prev, toolMode }));
  const tools = toolsSource.status === "ready" ? toolsSource.tools : [];
  const destructive = destructiveToolNames(tools);
  const toggleTool = (name: string, checked: boolean) =>
    onChange((prev) => {
      const selected = new Set(prev.selectedTools);
      if (checked) selected.add(name);
      else selected.delete(name);
      return { ...prev, selectedTools: [...selected] };
    });

  return (
    <section className="space-y-3">
      <div>
        <h3 className="text-sm font-medium">Tools</h3>
        <Text small muted>
          Which of this server&apos;s tools the guardrail inspects.
        </Text>
      </div>
      <RadioGroup
        value={state.toolMode}
        onValueChange={(value) => setToolMode(value as ServerToolMode)}
        className="space-y-2"
      >
        <label className="flex items-start gap-2.5">
          <RadioGroupItem value="all" className="mt-0.5" />
          <span className="text-sm">
            All tools
            {toolsSource.status === "ready" ? ` (${tools.length})` : ""}
            <Text small muted>
              Tools added to the server later are covered automatically.
            </Text>
          </span>
        </label>
        <label className="flex items-start gap-2.5">
          <RadioGroupItem value="selected" className="mt-0.5" />
          <span className="text-sm">
            Selected tools
            <Text small muted>
              Only the tools picked below. Others pass through uninspected.
            </Text>
          </span>
        </label>
      </RadioGroup>
      {state.toolMode === "selected" ? (
        <SelectedTools
          toolsSource={toolsSource}
          selected={state.selectedTools}
          destructive={destructive}
          onToggle={toggleTool}
          onSelectDestructive={() =>
            onChange((prev) => ({
              ...prev,
              selectedTools: [
                ...new Set([...prev.selectedTools, ...destructive]),
              ],
            }))
          }
        />
      ) : null}
    </section>
  );
}

function SelectedTools({
  toolsSource,
  selected,
  destructive,
  onToggle,
  onSelectDestructive,
}: {
  toolsSource: ToolsSource;
  selected: string[];
  destructive: string[];
  onToggle: (name: string, checked: boolean) => void;
  onSelectDestructive: () => void;
}): JSX.Element {
  const [search, setSearch] = useState("");
  if (toolsSource.status !== "ready") {
    return (
      <Text small muted className="flex items-center gap-2">
        <Loader2 className="size-4 animate-spin" /> Loading tools...
      </Text>
    );
  }
  const query = search.trim().toLowerCase();
  const visible = toolsSource.tools.filter((tool) =>
    tool.name.toLowerCase().includes(query),
  );
  return (
    <div className="space-y-2">
      <Input
        aria-label="Search tools"
        placeholder="Search tools…"
        value={search}
        onChange={setSearch}
      />
      <div className="border-border max-h-64 overflow-y-auto border">
        {visible.length === 0 ? (
          <Text small muted className="p-3">
            No matching tools.
          </Text>
        ) : (
          visible.map((tool) => (
            <label
              key={tool.name}
              className="border-muted flex items-center gap-3 border-b px-3 py-2 last:border-b-0"
            >
              <Checkbox
                aria-label={tool.name}
                checked={selected.includes(tool.name)}
                onCheckedChange={(checked) =>
                  onToggle(tool.name, checked === true)
                }
              />
              <code className="min-w-0 flex-1 truncate font-mono text-xs">
                {tool.name}
              </code>
              {tool.destructive ? (
                <span className="text-muted-foreground text-[11px]">
                  Destructive
                </span>
              ) : null}
            </label>
          ))
        )}
      </div>
      <div className="text-muted-foreground flex items-center justify-between text-xs">
        <span>
          {selected.length} of {toolsSource.tools.length} selected
        </span>
        {destructive.length > 0 ? (
          <button
            type="button"
            className="hover:text-foreground underline"
            onClick={onSelectDestructive}
          >
            Select all destructive ({destructive.length})
          </button>
        ) : null}
      </div>
    </div>
  );
}
