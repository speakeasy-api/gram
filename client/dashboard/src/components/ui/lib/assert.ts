export function assertNever(value: unknown): never {
  throw new Error(`Unhandled value: ${JSON.stringify(value)}`);
}

export function assert(condition: boolean, message: string): asserts condition {
  if (!condition) {
    throw new Error(message);
  }
}
