import { Info } from "lucide-react";
import { cn } from "@/lib/utils";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Popover,
  PopoverContent,
  PopoverDescription,
  PopoverHeader,
  PopoverTitle,
  PopoverTrigger,
} from "@/components/ui/popover";
import { Button } from "@/components/ui/button";
import {
  useGramMode,
  type EnvVarReadout as EnvVar,
} from "@/hooks/use-gram-mode";

/**
 * Reads `meta.env` off the gram-mode endpoint and renders one row per
 * documented variable. Each row has an (i) info button that opens a popover
 * with the full documentation — layout never shifts. Built as its own
 * component so we can grow the popover (links to source, examples, "where
 * this is read in Gram", etc.) without touching the home page.
 */
export function EnvReadout() {
  const { data, isLoading, error } = useGramMode();

  return (
    <Card size="sm" className="!rounded-md">
      <CardHeader>
        <CardTitle className="text-sm">Environment</CardTitle>
      </CardHeader>
      <CardContent>
        {isLoading && (
          <div className="text-xs text-muted-foreground">Loading…</div>
        )}
        {error && (
          <div className="text-xs text-destructive">
            {(error as Error).message}
          </div>
        )}
        {data && (
          <ul className="divide-y divide-border -my-2">
            {data.meta.env.map((v) => (
              <EnvRow key={v.name} variable={v} />
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  );
}

function EnvRow({ variable }: { variable: EnvVar }) {
  const valueDisplay = renderValue(variable);

  return (
    <li className="py-2 grid grid-cols-[1fr_auto] gap-2 items-center">
      <div className="min-w-0">
        <div className="text-[10px] font-mono uppercase tracking-wider text-muted-foreground/80">
          {variable.name}
        </div>
        <div
          className={cn(
            "text-sm font-mono truncate leading-tight",
            variable.is_set
              ? "text-foreground"
              : "text-muted-foreground italic",
          )}
          title={typeof valueDisplay === "string" ? valueDisplay : undefined}
        >
          {valueDisplay}
        </div>
      </div>
      <Popover>
        <PopoverTrigger
          render={
            <Button
              type="button"
              variant="ghost"
              size="icon-xs"
              aria-label={`Show docs for ${variable.name}`}
            />
          }
        >
          <Info className="text-muted-foreground" />
        </PopoverTrigger>
        <PopoverContent align="end" className="w-80">
          <PopoverHeader>
            <PopoverTitle className="font-mono text-sm">
              {variable.name}
            </PopoverTitle>
            <PopoverDescription className="text-xs">
              {variable.description}
            </PopoverDescription>
          </PopoverHeader>
          <div className="text-xs space-y-1">
            <div className="flex items-baseline gap-2">
              <span className="text-muted-foreground w-20 shrink-0">
                State:
              </span>
              <span
                className={cn(
                  "font-mono",
                  variable.is_set ? "text-foreground" : "text-muted-foreground",
                )}
              >
                {variable.is_set ? "set" : "unset"}
              </span>
            </div>
            <div className="flex items-baseline gap-2">
              <span className="text-muted-foreground w-20 shrink-0">
                Sensitive:
              </span>
              <span className="font-mono">
                {variable.sensitive ? "yes" : "no"}
              </span>
            </div>
            {!variable.sensitive && variable.value && (
              <div className="flex items-baseline gap-2">
                <span className="text-muted-foreground w-20 shrink-0">
                  Value:
                </span>
                <code className="text-foreground break-all">
                  {variable.value}
                </code>
              </div>
            )}
          </div>
        </PopoverContent>
      </Popover>
    </li>
  );
}

function renderValue(v: EnvVar): React.ReactNode {
  if (!v.is_set) return "unset";
  if (v.sensitive) return <span className="text-muted-foreground">[set]</span>;
  return v.value ?? "unset";
}
