import { useEffect, useState } from "react";
import { type CelEngine, loadCelEngine } from "./cel-wasm";

export type CelEngineState =
  | { status: "loading" }
  | { status: "ready"; engine: CelEngine }
  | { status: "error"; error: string };

/** Load the wasm CEL engine once and expose its state. The engine is the same
 *  celenv the server runs, so a compile here matches what the server accepts on
 *  save; on load failure the caller falls back to server-side validation.
 *  Pass `enabled: false` to defer the (large) wasm fetch until the surface
 *  that needs the engine is actually shown; the state stays "loading" until
 *  it flips to true. */
export function useCelEngine(enabled = true): CelEngineState {
  const [state, setState] = useState<CelEngineState>({ status: "loading" });

  useEffect(() => {
    if (!enabled) return;
    let alive = true;
    loadCelEngine().then(
      (engine) => alive && setState({ status: "ready", engine }),
      (err: unknown) =>
        alive &&
        setState({
          status: "error",
          error: err instanceof Error ? err.message : String(err),
        }),
    );
    return () => {
      alive = false;
    };
  }, [enabled]);

  return state;
}
