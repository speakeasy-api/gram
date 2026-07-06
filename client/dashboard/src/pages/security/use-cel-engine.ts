import { useEffect, useState } from "react";
import { type CelEngine, loadCelEngine } from "./cel-wasm";

export type CelEngineState =
  | { status: "loading" }
  | { status: "ready"; engine: CelEngine }
  | { status: "error"; error: string };

/** Load the wasm CEL engine once and expose its state. The engine is the same
 *  celenv the server runs, so a compile here matches what the server accepts on
 *  save; on load failure the caller falls back to server-side validation. */
export function useCelEngine(): CelEngineState {
  const [state, setState] = useState<CelEngineState>({ status: "loading" });

  useEffect(() => {
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
  }, []);

  return state;
}
