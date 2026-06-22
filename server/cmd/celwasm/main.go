//go:build js && wasm

// Command celwasm exposes the risk CEL engine to the browser. It is the exact
// same celenv the server compiles and evaluates with, built for GOOS=js
// GOARCH=wasm, so the dashboard editor type-checks expressions and previews the
// spans a rule would match against real engine semantics — with zero network
// round-trips and zero drift from what the backend enforces on save.
//
// Exposed globals (set once the module is instantiated and go.run() is called):
//
//	__celEngineReady : true        engine built; safe to call the funcs below
//	__celInitError   : string      set instead if the engine failed to build
//	__celReference() : json        the editor catalog (celenv.Describe())
//	__celCompile(expr) -> {ok, error?}
//	__celEval(expr, messageJSON) -> {ok, matched?, spans?(json), error?}
//
// The dashboard treats a missing __celEngineReady as "wasm unavailable" and
// falls back to the server's risk.compileExpr endpoint for validation.
package main

import (
	"encoding/json"
	"syscall/js"

	"github.com/google/cel-go/cel"
	"github.com/speakeasy-api/gram/server/internal/risk/celenv"
)

func main() {
	eng, err := celenv.New()
	if err != nil {
		js.Global().Set("__celInitError", js.ValueOf(err.Error()))
		return
	}

	h := &handle{eng: eng, programs: map[string]cel.Program{}}

	js.Global().Set("__celReference", js.FuncOf(h.reference))
	js.Global().Set("__celComplete", js.FuncOf(h.complete))
	js.Global().Set("__celCompile", js.FuncOf(h.compile))
	js.Global().Set("__celEval", js.FuncOf(h.eval))
	js.Global().Set("__celEngineReady", js.ValueOf(true))

	select {} // keep the Go runtime alive so the exported funcs stay callable
}

type handle struct {
	eng *celenv.Engine
	// programs caches compiled programs by source so repeated eval of the same
	// rule (the common case while an author tweaks a sample message) skips the
	// re-compile. Single-threaded wasm: no lock needed.
	programs map[string]cel.Program
}

// reference() -> JSON string of celenv.Describe(), seeding autocomplete and the
// reference panel from the same asset that does the type-checking.
func (h *handle) reference(_ js.Value, _ []js.Value) any {
	out, _ := json.Marshal(celenv.Describe())
	return js.ValueOf(string(out))
}

// complete(srcUpToCursor) -> JSON of celenv.Completion. Type-directed: the
// receiver's real type decides the offering, so the editor needs no completion
// heuristics of its own.
func (h *handle) complete(_ js.Value, args []js.Value) any {
	out, _ := json.Marshal(h.eng.Complete(args[0].String()))
	return js.ValueOf(string(out))
}

// compile(expr) -> {ok: bool, error?: string}. The validate-as-you-type path;
// mirrors the server's save-time gate exactly because it is the same engine.
func (h *handle) compile(_ js.Value, args []js.Value) any {
	expr := args[0].String()
	prg, err := h.eng.Compile(expr)
	if err != nil {
		return js.ValueOf(map[string]any{"ok": false, "error": err.Error()})
	}
	h.programs[expr] = prg
	return js.ValueOf(map[string]any{"ok": true})
}

// eval(expr, messageJSON) -> {ok, matched, spans} | {ok:false, error}.
// messageJSON is a celenv.Message ({content,type,tools:[{name,server,function,args}]}).
// spans is a JSON string the caller JSON.parses — this is the capability cel-js
// could not provide: real span extraction with byte offsets.
func (h *handle) eval(_ js.Value, args []js.Value) any {
	expr := args[0].String()

	var msg celenv.Message
	if err := json.Unmarshal([]byte(args[1].String()), &msg); err != nil {
		return js.ValueOf(map[string]any{"ok": false, "error": "bad message json: " + err.Error()})
	}

	prg, ok := h.programs[expr]
	if !ok {
		var err error
		if prg, err = h.eng.Compile(expr); err != nil {
			return js.ValueOf(map[string]any{"ok": false, "error": err.Error()})
		}
		h.programs[expr] = prg
	}

	spans, matched, err := h.eng.EvalDetection(prg, msg)
	if err != nil {
		return js.ValueOf(map[string]any{"ok": false, "error": err.Error()})
	}

	out, _ := json.Marshal(spans)
	return js.ValueOf(map[string]any{
		"ok":      true,
		"matched": matched,
		"spans":   string(out),
	})
}
