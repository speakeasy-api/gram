import type { CommandContext } from "@stricli/core";

export interface LocalContext extends CommandContext {
  readonly process: NodeJS.Process;
}

export function buildContext(process: NodeJS.Process): LocalContext {
  return { process };
}
