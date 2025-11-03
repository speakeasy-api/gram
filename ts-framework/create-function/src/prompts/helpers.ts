import {
  confirm,
  select,
  text,
  type ConfirmOptions,
  type SelectOptions,
  type TextOptions,
} from "@clack/prompts";

const y = new Set(["y", "yes", "true", "t", "1"]);
export function yn(value: boolean | string | undefined): boolean | undefined {
  if (value == null) return value;
  if (typeof value === "boolean") return value;
  return y.has(value.toLowerCase());
}

export function textOrClack(
  options: TextOptions,
): (value: string | undefined) => Promise<string | symbol> {
  return async (value: string | undefined) => value || text(options);
}

export function selectOrClack<T>(
  options: SelectOptions<T>,
): (value: T | undefined) => Promise<T | symbol> {
  return async (value: T | undefined) => value || select(options);
}

export function confirmOrClack(
  options: ConfirmOptions,
): (value: boolean | undefined) => Promise<boolean | symbol> {
  return async (value: boolean | undefined) => {
    if (value != null) {
      return value;
    }

    return confirm(options);
  };
}
