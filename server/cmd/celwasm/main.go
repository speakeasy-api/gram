//go:build js && wasm

// Command celwasm exposes the risk CEL engine to the browser (the same celenv
// the server runs), built for GOOS=js GOARCH=wasm. It sets globals
// __celEngineReady (or __celInitError) and the funcs __celReference /
// __celCompile / __celComplete / __celEval.
package main

import (
	"encoding/json"
	"syscall/js"

	"github.com/google/cel-go/cel"
	"github.com/speakeasy-api/gram/server/internal/risk/celenv"
)

// maxPrograms bounds the compiled-program cache so a long session can't grow
// browser memory without limit.
const maxPrograms = 256

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
	eng      *celenv.Engine
	programs map[string]cel.Program // compiled-program cache; single-threaded wasm, no lock
}

// arg returns args[i] as a string, or "" when the JS caller passed too few args
// (which would otherwise panic the module).
func arg(args []js.Value, i int) string {
	if i >= len(args) {
		return ""
	}
	return args[i].String()
}

func (h *handle) put(expr string, prg cel.Program) {
	if len(h.programs) >= maxPrograms {
		h.programs = map[string]cel.Program{}
	}
	h.programs[expr] = prg
}

func (h *handle) reference(_ js.Value, _ []js.Value) any {
	out, _ := json.Marshal(celenv.Describe())
	return js.ValueOf(string(out))
}

func (h *handle) complete(_ js.Value, args []js.Value) any {
	out, _ := json.Marshal(h.eng.Complete(arg(args, 0)))
	return js.ValueOf(string(out))
}

func (h *handle) compile(_ js.Value, args []js.Value) any {
	expr := arg(args, 0)
	prg, err := h.eng.Compile(expr)
	if err != nil {
		return js.ValueOf(map[string]any{"ok": false, "error": err.Error()})
	}
	h.put(expr, prg)
	return js.ValueOf(map[string]any{"ok": true})
}

// eval(expr, messageJSON) -> {ok, matched, spans(json)} | {ok:false, error}.
func (h *handle) eval(_ js.Value, args []js.Value) any {
	expr := arg(args, 0)

	var msg celenv.Message
	if err := json.Unmarshal([]byte(arg(args, 1)), &msg); err != nil {
		return js.ValueOf(map[string]any{"ok": false, "error": "bad message json: " + err.Error()})
	}

	prg, ok := h.programs[expr]
	if !ok {
		var err error
		if prg, err = h.eng.Compile(expr); err != nil {
			return js.ValueOf(map[string]any{"ok": false, "error": err.Error()})
		}
		h.put(expr, prg)
	}

	spans, matched, err := h.eng.EvalDetection(prg, msg)
	if err != nil {
		return js.ValueOf(map[string]any{"ok": false, "error": err.Error()})
	}

	out, _ := json.Marshal(spans)
	return js.ValueOf(map[string]any{"ok": true, "matched": matched, "spans": string(out)})
}
