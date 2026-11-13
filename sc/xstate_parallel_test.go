package sc_test

// This file ports XState's parallel state tests to sc, to compare the two
// libraries' semantics:
//
//	packages/core/test/parallel.test.ts
//	packages/core/test/parallelConformance.test.ts
//
// (XState 6.0.0-beta.2). Each test names the XState case it ports. Where sc
// behaves differently the test asserts what sc does and says so in a comment
// starting "DIVERGES from XState:".
//
// Three things are translated the same way throughout:
//
//   - sc state identifiers are unique in a chart, so a state that shares its
//     name with another is given its path, such as "bold.off".
//   - The root of an sc chart is always a compound state, so an XState
//     machine whose root is parallel is wrapped in one top-level parallel
//     state, which then shows up first in Snapshot.Active.
//   - XState's state value objects are compared as the list of active states
//     in document order.

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/sc"
)

// xparCtx is the context of the charts in this file.
type xparCtx struct {
	// Value is what the CHANGE event of the editor chart stores.
	Value string
}

type xparBuilder = sc.Builder[string, xparCtx]

type (
	xparToggleBold      struct{}
	xparToggleUnderline struct{}
	xparToggleItalics   struct{}
	xparBullets         struct{}
	xparNumbers         struct{}
	xparNone            struct{}
	xparReset           struct{}

	xparWak1 struct{}
	xparWak2 struct{}

	xparE struct{}

	xparSingleClickActivity       struct{}
	xparSingleClickLink           struct{}
	xparSingleClickCanvas         struct{}
	xparSwitchToProjectManagement struct{}
	xparSwitchToStructureEdit     struct{}
	xparCutInClipboardSuccess     struct{}
	xparCopyInClipboardSuccess    struct{}
	xparPasteFromClipboardSuccess struct{}

	xparClear   struct{}
	xparTurnOn  struct{}
	xparTurnOff struct{}
	xparOuter1A struct{}
	xparOuter1B struct{}
	xparOuter1C struct{}

	xparChange struct{ Value string }
	xparSave   struct{}

	xparNext struct{}

	xparEventComplex struct{}
	xparEventSimple  struct{}
	xparNtj0Work     struct{}
	xparNtj0Idle     struct{}
	xparNtj1Work     struct{}
	xparNtj1Idle     struct{}

	xparToA struct{}
	xparToB struct{}

	xparLowerA struct{}
	xparLowerB struct{}
	xparLowerC struct{}

	xparUpdate struct{}

	xparToggle        struct{}
	xparGoToDashboard struct{}

	xparGotoFoobaz struct{}

	xparFinish    struct{}
	xparFinishOne struct{}
	xparFinishTwo struct{}

	xparToggleMode struct{}
	xparMyEvent    struct{}

	xparUploaded struct{}
	xparValid    struct{}
	xparDoneA    struct{}
	xparDoneB    struct{}
	xparDone     struct{}

	xparArchive  struct{}
	xparEdit     struct{}
	xparReenterA struct{}
)

func xparNoop(context.Context, xparCtx) {}

// xparDo is a transition action that does nothing.
func xparDo[Ev any](context.Context, xparCtx, Ev) {}

// xparTracked gives a state the entry and exit actions that XState's
// trackEntries test helper adds to every state.
func xparTracked(s *sc.StateBuilder[string, xparCtx]) *sc.StateBuilder[string, xparCtx] {
	return s.Entry("enter", xparNoop).Exit("exit", xparNoop)
}

// xparTrace lists the entries and exits of tracked states among effects, in
// the form trackEntries reports them.
func xparTrace(effects []sc.Effect[xparCtx]) []string {
	out := []string{}
	for _, e := range effects {
		if e.Kind != sc.EffectAction {
			continue
		}
		if e.Name == "enter" || e.Name == "exit" {
			out = append(out, e.Name+": "+e.State)
		}
	}

	return out
}

// xparCount counts the effects named name.
func xparCount(effects []sc.Effect[xparCtx], name string) int {
	n := 0
	for _, got := range names(effects) {
		if got == name {
			n++
		}
	}

	return n
}

// xparRaise returns the function an entry raise needs to raise ev.
func xparRaise(ev any) func(xparCtx) any {
	return func(xparCtx) any { return ev }
}

// xparWordMachine is XState's wordMachine.
func xparWordMachine(t *testing.T) *sc.Machine[string, xparCtx] {
	t.Helper()

	m, err := sc.New("word", func(b *xparBuilder) {
		b.Initial("word")
		b.Parallel("word", func(b *xparBuilder) {
			b.State("bold", func(b *xparBuilder) {
				b.Initial("bold.off")
				b.State("bold.on").On[xparToggleBold]().To("bold.off")
				b.State("bold.off").On[xparToggleBold]().To("bold.on")
			})
			b.State("underline", func(b *xparBuilder) {
				b.Initial("underline.off")
				b.State("underline.on").On[xparToggleUnderline]().To("underline.off")
				b.State("underline.off").On[xparToggleUnderline]().To("underline.on")
			})
			b.State("italics", func(b *xparBuilder) {
				b.Initial("italics.off")
				b.State("italics.on").On[xparToggleItalics]().To("italics.off")
				b.State("italics.off").On[xparToggleItalics]().To("italics.on")
			})
			b.State("list", func(b *xparBuilder) {
				b.Initial("none")
				none := b.State("none")
				none.On[xparBullets]().To("bullets")
				none.On[xparNumbers]().To("numbers")
				bullets := b.State("bullets")
				bullets.On[xparNone]().To("none")
				bullets.On[xparNumbers]().To("numbers")
				numbers := b.State("numbers")
				numbers.On[xparBullets]().To("bullets")
				numbers.On[xparNone]().To("none")
			})
		}).On[xparReset]().To("word")
	})
	require.NoError(t, err)

	return m
}

// xparWordValue is the active states of the word chart for the given leaf
// of each region.
func xparWordValue(bold, underline, italics, list string) []string {
	return []string{"word", "bold", "bold." + bold, "underline", "underline." + underline, "italics", "italics." + italics, "list", list}
}

// XState: parallel states > should have initial parallel states
func TestXParShouldHaveInitialParallelStates(t *testing.T) {
	t.Parallel()

	snap, _ := xparWordMachine(t).Start(xparCtx{}, epoch)

	require.Equal(t, xparWordValue("off", "off", "off", "none"), snap.Active())
}

// XState: parallel states > should go from <state> to <state> on <events>
// (one generated case per subtest). XState resolves a partial state value
// such as {"bold": "off"} by filling the other regions with their initial
// states; StartIn is given the resolved leaves.
func TestXParShouldGoFromStateToStateOnEvents(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		from   []string
		events []any
		want   []string
	}{
		{
			// XState: should go from {"bold": "off"} to {"bold":"on","italics":"off","underline":"off","list":"none"} on TOGGLE_BOLD
			name:   `from {"bold": "off"} on TOGGLE_BOLD`,
			from:   []string{"bold.off", "underline.off", "italics.off", "none"},
			events: []any{xparToggleBold{}},
			want:   xparWordValue("on", "off", "off", "none"),
		},
		{
			// XState: should go from {"bold": "on"} to {"bold":"off","italics":"off","underline":"off","list":"none"} on TOGGLE_BOLD
			name:   `from {"bold": "on"} on TOGGLE_BOLD`,
			from:   []string{"bold.on", "underline.off", "italics.off", "none"},
			events: []any{xparToggleBold{}},
			want:   xparWordValue("off", "off", "off", "none"),
		},
		{
			// XState: should go from {"bold":"off","italics":"off","underline":"on","list":"bullets"} to {"bold":"on","italics":"on","underline":"on","list":"bullets"} on TOGGLE_BOLD, TOGGLE_ITALICS
			name:   `from underline on and bullets on TOGGLE_BOLD, TOGGLE_ITALICS`,
			from:   []string{"bold.off", "underline.on", "italics.off", "bullets"},
			events: []any{xparToggleBold{}, xparToggleItalics{}},
			want:   xparWordValue("on", "on", "on", "bullets"),
		},
		{
			// XState: should go from {"bold":"off","italics":"off","underline":"on","list":"bullets"} to {"bold":"off","italics":"off","underline":"off","list":"none"} on RESET
			name:   `from underline on and bullets on RESET`,
			from:   []string{"bold.off", "underline.on", "italics.off", "bullets"},
			events: []any{xparReset{}},
			want:   xparWordValue("off", "off", "off", "none"),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			m := xparWordMachine(t)
			snap, _, err := m.StartIn(xparCtx{}, epoch, tc.from...)
			require.NoError(t, err)

			for _, ev := range tc.events {
				snap, _ = m.Transition(snap, ev, epoch)
			}

			require.Equal(t, tc.want, snap.Active())
		})
	}
}

// XState: parallel states > should have all parallel states represented in the state value
func TestXParShouldHaveAllParallelStatesRepresentedInTheStateValue(t *testing.T) {
	t.Parallel()

	m, err := sc.New("wak", func(b *xparBuilder) {
		b.Initial("root")
		b.Parallel("root", func(b *xparBuilder) {
			b.State("wak1", func(b *xparBuilder) {
				b.Initial("wak1sonA")
				b.State("wak1sonA")
				b.State("wak1sonB")
			}).On[xparWak1]().To("wak1sonB")
			b.State("wak2", func(b *xparBuilder) {
				b.Initial("wak2sonA")
				b.State("wak2sonA")
			})
		})
	})
	require.NoError(t, err)

	snap, _ := m.Start(xparCtx{}, epoch)
	snap, _ = m.Transition(snap, xparWak1{}, epoch)

	require.Equal(t, []string{"root", "wak1", "wak1sonB", "wak2", "wak2sonA"}, snap.Active())
}

// XState: parallel states > should have all parallel states represented in the state value (2)
func TestXParShouldHaveAllParallelStatesRepresentedInTheStateValue2(t *testing.T) {
	t.Parallel()

	// XState's wakMachine, with its entry and exit actions.
	m, err := sc.New("wakMachine", func(b *xparBuilder) {
		b.Initial("wakMachine")
		b.Parallel("wakMachine", func(b *xparBuilder) {
			b.State("wak1", func(b *xparBuilder) {
				b.Initial("wak1sonA")
				b.State("wak1sonA").Entry("wak1sonAenter", xparNoop).Exit("wak1sonAexit", xparNoop)
				b.State("wak1sonB").Entry("wak1sonBenter", xparNoop).Exit("wak1sonBexit", xparNoop)
			}).Entry("wak1enter", xparNoop).Exit("wak1exit", xparNoop).
				On[xparWak1]().To("wak1sonB")
			b.State("wak2", func(b *xparBuilder) {
				b.Initial("wak2sonA")
				b.State("wak2sonA").Entry("wak2sonAenter", xparNoop).Exit("wak2sonAexit", xparNoop)
				b.State("wak2sonB").Entry("wak2sonBenter", xparNoop).Exit("wak2sonBexit", xparNoop)
			}).Entry("wak2enter", xparNoop).Exit("wak2exit", xparNoop).
				On[xparWak2]().To("wak2sonB")
		})
	})
	require.NoError(t, err)

	snap, _ := m.Start(xparCtx{}, epoch)
	snap, effects := m.Transition(snap, xparWak2{}, epoch)

	require.Equal(t, []string{"wakMachine", "wak1", "wak1sonA", "wak2", "wak2sonB"}, snap.Active())
	// Not asserted by XState: only the children of wak2 change.
	require.Equal(t, []string{"wak2sonA/wak2sonAexit", "wak2sonB/wak2sonBenter"}, names(effects))
}

// xparFlatParallelMachine is XState's flatParallelMachine.
func xparFlatParallelMachine(t *testing.T) *sc.Machine[string, xparCtx] {
	t.Helper()

	m, err := sc.New("flat", func(b *xparBuilder) {
		b.Initial("root")
		b.Parallel("root", func(b *xparBuilder) {
			b.State("foo")
			b.State("bar")
			b.State("baz", func(b *xparBuilder) {
				b.Initial("one")
				b.State("one").On[xparE]().To("two")
				b.State("two")
			})
		})
	})
	require.NoError(t, err)

	return m
}

// XState: parallel states > should work with regions without states
// (the first of two cases with this title: the initial state).
func TestXParShouldWorkWithRegionsWithoutStatesInitially(t *testing.T) {
	t.Parallel()

	snap, _ := xparFlatParallelMachine(t).Start(xparCtx{}, epoch)

	require.Equal(t, []string{"root", "foo", "bar", "baz", "one"}, snap.Active())
}

// XState: parallel states > should work with regions without states
// (the second of two cases with this title: after an event).
func TestXParShouldWorkWithRegionsWithoutStatesAfterAnEvent(t *testing.T) {
	t.Parallel()

	m := xparFlatParallelMachine(t)
	snap, _ := m.Start(xparCtx{}, epoch)
	snap, _ = m.Transition(snap, xparE{}, epoch)

	require.Equal(t, []string{"root", "foo", "bar", "baz", "two"}, snap.Active())
}

// XState: parallel states > should properly transition to relative substate
func TestXParShouldProperlyTransitionToRelativeSubstate(t *testing.T) {
	t.Parallel()

	// XState's composerMachine. The states of the two editing modes share
	// names in XState and are prefixed SE and PM here.
	m, err := sc.New("composer", func(b *xparBuilder) {
		b.Initial("ReadOnly")
		b.State("ReadOnly", func(b *xparBuilder) {
			b.Initial("StructureEdit")

			b.Parallel("StructureEdit", func(b *xparBuilder) {
				selection := b.State("SE.SelectionStatus", func(b *xparBuilder) {
					b.Initial("SE.SelectedNone")
					b.State("SE.SelectedNone").Entry("redraw", xparNoop)
					b.State("SE.SelectedActivity").Entry("redraw", xparNoop).
						On[xparSingleClickCanvas]().Do("selectNone", xparDo[xparSingleClickCanvas]).To("SE.SelectedNone")
					b.State("SE.SelectedLink").Entry("redraw", xparNoop).
						On[xparSingleClickCanvas]().Do("selectNone", xparDo[xparSingleClickCanvas]).To("SE.SelectedNone")
				})
				selection.On[xparSingleClickActivity]().
					Do("selectActivity", xparDo[xparSingleClickActivity]).To("SE.SelectedActivity")
				selection.On[xparSingleClickLink]().
					Do("selectLink", xparDo[xparSingleClickLink]).To("SE.SelectedLink")

				b.State("SE.ClipboardStatus", func(b *xparBuilder) {
					b.Initial("Empty")
					empty := b.State("Empty").Entry("emptyClipboard", xparNoop)
					empty.On[xparCutInClipboardSuccess]().To("FilledByCut")
					empty.On[xparCopyInClipboardSuccess]().To("FilledByCopy")
					byCopy := b.State("FilledByCopy")
					byCopy.On[xparCutInClipboardSuccess]().To("FilledByCut")
					byCopy.On[xparCopyInClipboardSuccess]().To("FilledByCopy")
					byCopy.On[xparPasteFromClipboardSuccess]().To("FilledByCopy")
					byCut := b.State("FilledByCut")
					byCut.On[xparCutInClipboardSuccess]().To("FilledByCut")
					byCut.On[xparCopyInClipboardSuccess]().To("FilledByCopy")
					byCut.On[xparPasteFromClipboardSuccess]().To("Empty")
				})
			}).On[xparSwitchToProjectManagement]().To("ProjectManagement")

			b.Parallel("ProjectManagement", func(b *xparBuilder) {
				selection := b.State("PM.SelectionStatus", func(b *xparBuilder) {
					b.Initial("PM.SelectedNone")
					b.State("PM.SelectedNone").Entry("redraw", xparNoop)
					b.State("PM.SelectedActivity").Entry("redraw", xparNoop).
						On[xparSingleClickCanvas]().To("PM.SelectedNone")
					b.State("PM.SelectedLink").Entry("redraw", xparNoop).
						On[xparSingleClickCanvas]().Do("selectNone", xparDo[xparSingleClickCanvas]).To("PM.SelectedNone")
				})
				selection.On[xparSingleClickActivity]().
					Do("selectActivity", xparDo[xparSingleClickActivity]).To("PM.SelectedActivity")
				selection.On[xparSingleClickLink]().
					Do("selectLink", xparDo[xparSingleClickLink]).To("PM.SelectedLink")
			}).On[xparSwitchToStructureEdit]().To("StructureEdit")
		}).Entry("selectNone", xparNoop)
	})
	require.NoError(t, err)

	snap, _ := m.Start(xparCtx{}, epoch)
	snap, _ = m.Transition(snap, xparSingleClickActivity{}, epoch)

	require.Equal(t,
		[]string{"ReadOnly", "StructureEdit", "SE.SelectionStatus", "SE.SelectedActivity", "SE.ClipboardStatus", "Empty"},
		snap.Active(),
	)
}

// XState: parallel states > should properly transition according to entry events on an initial state
func TestXParShouldProperlyTransitionAccordingToEntryEventsOnAnInitialState(t *testing.T) {
	t.Parallel()

	m, err := sc.New("entryRaise", func(b *xparBuilder) {
		b.Initial("root")
		b.Parallel("root", func(b *xparBuilder) {
			b.State("OUTER1", func(b *xparBuilder) {
				b.Initial("B")
				b.State("A")
				b.State("B").RaiseOnEntry("raiseClear", xparRaise(xparClear{}))
			})
			b.Parallel("OUTER2", func(b *xparBuilder) {
				b.State("INNER1", func(b *xparBuilder) {
					b.Initial("INNER1.ON")
					b.State("INNER1.OFF")
					b.State("INNER1.ON").On[xparClear]().To("INNER1.OFF")
				})
				b.State("INNER2", func(b *xparBuilder) {
					b.Initial("INNER2.OFF")
					b.State("INNER2.OFF")
					b.State("INNER2.ON")
				})
			})
		})
	}, sc.AllowUnreachable())
	require.NoError(t, err)

	snap, _ := m.Start(xparCtx{}, epoch)

	require.Equal(t,
		[]string{"root", "OUTER1", "B", "OUTER2", "INNER1", "INNER1.OFF", "INNER2", "INNER2.OFF"},
		snap.Active(),
	)
}

// XState: parallel states > should properly transition when raising events for a parallel state
func TestXParShouldProperlyTransitionWhenRaisingEventsForAParallelState(t *testing.T) {
	t.Parallel()

	// XState's raisingParallelMachine.
	m, err := sc.New("raising", func(b *xparBuilder) {
		b.Initial("root")
		b.Parallel("root", func(b *xparBuilder) {
			b.State("OUTER1", func(b *xparBuilder) {
				b.Initial("C")
				a := b.State("A")
				a.RaiseOnEntry("raiseTurnOff", xparRaise(xparTurnOff{}))
				a.On[xparOuter1B]().To("B")
				a.On[xparOuter1C]().To("C")
				bb := b.State("B")
				bb.RaiseOnEntry("raiseTurnOn", xparRaise(xparTurnOn{}))
				bb.On[xparOuter1A]().To("A")
				bb.On[xparOuter1C]().To("C")
				c := b.State("C")
				c.RaiseOnEntry("raiseClear", xparRaise(xparClear{}))
				c.On[xparOuter1A]().To("A")
				c.On[xparOuter1B]().To("B")
			})
			b.Parallel("OUTER2", func(b *xparBuilder) {
				b.State("INNER1", func(b *xparBuilder) {
					b.Initial("INNER1.ON")
					b.State("INNER1.OFF").On[xparTurnOn]().To("INNER1.ON")
					b.State("INNER1.ON").On[xparClear]().To("INNER1.OFF")
				})
				b.State("INNER2", func(b *xparBuilder) {
					b.Initial("INNER2.OFF")
					b.State("INNER2.OFF").On[xparTurnOn]().To("INNER2.ON")
					b.State("INNER2.ON").On[xparTurnOff]().To("INNER2.OFF")
				})
			})
		})
	})
	require.NoError(t, err)

	snap, _ := m.Start(xparCtx{}, epoch)
	// Not asserted by XState: the CLEAR raised on entering C turned INNER1 off.
	require.True(t, snap.In("INNER1.OFF"))

	snap, _ = m.Transition(snap, xparOuter1B{}, epoch)

	require.Equal(t,
		[]string{"root", "OUTER1", "B", "OUTER2", "INNER1", "INNER1.ON", "INNER2", "INNER2.ON"},
		snap.Active(),
	)
}

// XState: parallel states > should handle simultaneous orthogonal transitions
func TestXParShouldHandleSimultaneousOrthogonalTransitions(t *testing.T) {
	t.Parallel()

	m, err := sc.New("yamlEditor", func(b *xparBuilder) {
		b.Initial("yamlEditor")
		b.Parallel("yamlEditor", func(b *xparBuilder) {
			b.State("editing").On[xparChange]().
				Assign("setValue", func(c xparCtx, e xparChange) xparCtx { c.Value = e.Value; return c })
			b.State("status", func(b *xparBuilder) {
				b.Initial("unsaved")
				b.State("unsaved").On[xparSave]().To("saved")
				b.State("saved").On[xparChange]().To("unsaved")
			})
		})
	})
	require.NoError(t, err)

	snap, _ := m.Start(xparCtx{}, epoch)
	snap, _ = m.Transition(snap, xparSave{}, epoch)
	snap, _ = m.Transition(snap, xparChange{Value: "something"}, epoch)

	require.Equal(t, []string{"yamlEditor", "editing", "status", "unsaved"}, snap.Active())
	require.Equal(t, "something", snap.Context.Value)
}

// XState: parallel states > should execute actions of the initial transition of a parallel region when entering the initial state nodes of a machine
func TestXParShouldExecuteActionsOfARegionWhenEnteringTheInitialStateNodesOfAMachine(t *testing.T) {
	t.Parallel()

	m, err := sc.New("initialActions", func(b *xparBuilder) {
		b.Initial("root")
		b.Parallel("root", func(b *xparBuilder) {
			b.State("a", func(b *xparBuilder) {
				b.Initial("a1")
				b.State("a1")
			}).Entry("spy", xparNoop)
		})
	})
	require.NoError(t, err)

	_, effects := m.Start(xparCtx{}, epoch)

	require.Equal(t, 1, xparCount(effects, "a/spy"))
}

// XState: parallel states > should execute actions of the initial transition of a parallel region when the parallel state is targeted with an explicit transition
func TestXParShouldExecuteActionsOfARegionWhenTheParallelStateIsTargetedExplicitly(t *testing.T) {
	t.Parallel()

	m, err := sc.New("explicitActions", func(b *xparBuilder) {
		b.Initial("a")
		b.State("a").On[xparNext]().To("b")
		b.Parallel("b", func(b *xparBuilder) {
			b.State("c", func(b *xparBuilder) {
				b.Initial("c1")
				b.State("c1")
			}).Entry("spy", xparNoop)
		}).Entry("bEntry", xparNoop)
	})
	require.NoError(t, err)

	snap, effects := m.Start(xparCtx{}, epoch)
	require.Zero(t, xparCount(effects, "c/spy"))

	_, effects = m.Transition(snap, xparNext{}, epoch)

	require.Equal(t, 1, xparCount(effects, "c/spy"))
}

// xparNestedParallelState is XState's nestedParallelState. STATE_OFF and
// STATE_ON name several states in XState and carry their path here.
func xparNestedParallelState(t *testing.T) *sc.Machine[string, xparCtx] {
	t.Helper()

	m, err := sc.New("nested", func(b *xparBuilder) {
		b.Initial("root")
		b.Parallel("root", func(b *xparBuilder) {
			b.State("OUTER1", func(b *xparBuilder) {
				b.Initial("OUTER1.STATE_OFF")
				off := b.State("OUTER1.STATE_OFF")
				off.On[xparEventComplex]().To("OUTER1.STATE_ON")
				off.On[xparEventSimple]().To("OUTER1.STATE_ON")
				b.Parallel("OUTER1.STATE_ON", func(b *xparBuilder) {
					b.State("STATE_NTJ0", func(b *xparBuilder) {
						b.Initial("STATE_IDLE_0")
						b.State("STATE_IDLE_0").On[xparNtj0Work]().To("STATE_WORKING_0")
						b.State("STATE_WORKING_0").On[xparNtj0Idle]().To("STATE_IDLE_0")
					})
					b.State("STATE_NTJ1", func(b *xparBuilder) {
						b.Initial("STATE_IDLE_1")
						b.State("STATE_IDLE_1").On[xparNtj1Work]().To("STATE_WORKING_1")
						b.State("STATE_WORKING_1").On[xparNtj1Idle]().To("STATE_IDLE_1")
					})
				})
			})
			b.State("OUTER2", func(b *xparBuilder) {
				b.Initial("OUTER2.STATE_OFF")
				off := b.State("OUTER2.STATE_OFF")
				off.On[xparEventComplex]().To("STATE_ON_COMPLEX")
				off.On[xparEventSimple]().To("STATE_ON_SIMPLE")
				b.State("STATE_ON_SIMPLE")
				b.Parallel("STATE_ON_COMPLEX", func(b *xparBuilder) {
					b.State("STATE_INNER1", func(b *xparBuilder) {
						b.Initial("STATE_INNER1.STATE_OFF")
						b.State("STATE_INNER1.STATE_OFF")
						b.State("STATE_INNER1.STATE_ON")
					})
					b.State("STATE_INNER2", func(b *xparBuilder) {
						b.Initial("STATE_INNER2.STATE_OFF")
						b.State("STATE_INNER2.STATE_OFF")
						b.State("STATE_INNER2.STATE_ON")
					})
				})
			})
		})
	}, sc.AllowUnreachable())
	require.NoError(t, err)

	return m
}

// XState: parallel states > transitions with nested parallel states > should properly transition when in a simple nested state
func TestXParShouldProperlyTransitionWhenInASimpleNestedState(t *testing.T) {
	t.Parallel()

	m := xparNestedParallelState(t)
	snap, _ := m.Start(xparCtx{}, epoch)
	snap, _ = m.Transition(snap, xparEventSimple{}, epoch)
	snap, _ = m.Transition(snap, xparNtj0Work{}, epoch)

	require.Equal(t,
		[]string{
			"root",
			"OUTER1", "OUTER1.STATE_ON", "STATE_NTJ0", "STATE_WORKING_0", "STATE_NTJ1", "STATE_IDLE_1",
			"OUTER2", "STATE_ON_SIMPLE",
		},
		snap.Active(),
	)
}

// XState: parallel states > transitions with nested parallel states > should properly transition when in a complex nested state
func TestXParShouldProperlyTransitionWhenInAComplexNestedState(t *testing.T) {
	t.Parallel()

	m := xparNestedParallelState(t)
	snap, _ := m.Start(xparCtx{}, epoch)
	snap, _ = m.Transition(snap, xparEventComplex{}, epoch)
	snap, _ = m.Transition(snap, xparNtj0Work{}, epoch)

	require.Equal(t,
		[]string{
			"root",
			"OUTER1", "OUTER1.STATE_ON", "STATE_NTJ0", "STATE_WORKING_0", "STATE_NTJ1", "STATE_IDLE_1",
			"OUTER2", "STATE_ON_COMPLEX", "STATE_INNER1", "STATE_INNER1.STATE_OFF", "STATE_INNER2", "STATE_INNER2.STATE_OFF",
		},
		snap.Active(),
	)
}

// XState: parallel states > nested flat parallel states > should represent the flat nested parallel states in the state value
//
// The XState machine declares its to-A transition on the root. An sc chart's
// root takes no transitions, so the chart is wrapped in a compound state.
func TestXParShouldRepresentTheFlatNestedParallelStatesInTheStateValue(t *testing.T) {
	t.Parallel()

	m, err := sc.New("flatNested", func(b *xparBuilder) {
		b.Initial("root")
		b.State("root", func(b *xparBuilder) {
			b.Initial("A")
			b.State("A").On[xparToB]().To("B")
			b.Parallel("B", func(b *xparBuilder) {
				b.State("C")
				b.State("D")
			})
		}).On[xparToA]().To("A")
	})
	require.NoError(t, err)

	snap, _ := m.Start(xparCtx{}, epoch)
	snap, _ = m.Transition(snap, xparToB{}, epoch)

	require.Equal(t, []string{"root", "B", "C", "D"}, snap.Active())
}

// XState: parallel states > deep flat parallel states > should properly evaluate deep flat parallel states
func TestXParShouldProperlyEvaluateDeepFlatParallelStates(t *testing.T) {
	t.Parallel()

	// XState's deepFlatParallelMachine.
	m, err := sc.New("deepFlat", func(b *xparBuilder) {
		b.Initial("root")
		b.Parallel("root", func(b *xparBuilder) {
			b.State("X")
			v := b.State("V", func(b *xparBuilder) {
				b.Initial("A")
				b.State("A")
				b.State("B", func(b *xparBuilder) {
					b.Initial("BB")
					b.Parallel("BB", func(b *xparBuilder) {
						b.State("BBB_A")
						b.State("BBB_B")
					})
				})
				b.State("C")
			})
			v.On[xparLowerA]().To("A")
			v.On[xparLowerB]().To("B")
			v.On[xparLowerC]().To("C")
		})
	})
	require.NoError(t, err)

	snap, _ := m.Start(xparCtx{}, epoch)
	snap, _ = m.Transition(snap, xparLowerA{}, epoch)
	snap, _ = m.Transition(snap, xparLowerC{}, epoch)
	snap, _ = m.Transition(snap, xparLowerB{}, epoch)

	require.Equal(t, []string{"root", "X", "V", "B", "BB", "BBB_A", "BBB_B"}, snap.Active())
}

// XState: parallel states > deep flat parallel states > should not overlap resolved state nodes in state resolution
//
// XState's transition on foo is a function that returns nothing; here it is
// a transition that stays.
func TestXParShouldNotOverlapResolvedStateNodesInStateResolution(t *testing.T) {
	t.Parallel()

	m, err := sc.New("pipeline", func(b *xparBuilder) {
		b.Initial("pipeline")
		b.Parallel("pipeline", func(b *xparBuilder) {
			b.State("foo").On[xparUpdate]().Stay()
			b.State("bar", func(b *xparBuilder) {
				b.Initial("idle")
				b.State("idle")
				b.State("baz")
			}).On[xparUpdate]().To("baz")
		})
	})
	require.NoError(t, err)

	snap, _ := m.Start(xparCtx{}, epoch)

	require.NotPanics(t, func() { snap, _ = m.Transition(snap, xparUpdate{}, epoch) })
	// Not asserted by XState: both regions took their transition.
	require.Equal(t, []string{"pipeline", "foo", "bar", "baz"}, snap.Active())
}

// XState: parallel states > other > regions should be able to transition to orthogonal regions
func TestXParRegionsShouldBeAbleToTransitionToOrthogonalRegions(t *testing.T) {
	t.Parallel()

	m, err := sc.New("orthogonal", func(b *xparBuilder) {
		b.Initial("root")
		b.Parallel("root", func(b *xparBuilder) {
			b.State("Pages", func(b *xparBuilder) {
				b.Initial("About")
				b.State("About")
				b.State("Dashboard")
			})
			b.State("Menu", func(b *xparBuilder) {
				b.Initial("Closed")
				b.State("Closed").On[xparToggle]().To("Opened")
				opened := b.State("Opened")
				opened.On[xparToggle]().To("Closed")
				opened.On[xparGoToDashboard]().To("Dashboard", "Opened")
			})
		})
	})
	require.NoError(t, err)

	snap, _ := m.Start(xparCtx{}, epoch)
	snap, _ = m.Transition(snap, xparToggle{}, epoch)
	snap, _ = m.Transition(snap, xparGoToDashboard{}, epoch)

	require.True(t, snap.In("Opened"))
	require.True(t, snap.In("Dashboard"))
}

// XState: parallel states > other > should calculate the entry set for reentering transitions in parallel states
//
// XState's entry action appends to a log in the context. An sc entry action
// cannot assign, so the entries of foobaz are counted from the effects.
func TestXParShouldCalculateTheEntrySetForReenteringTransitionsInParallelStates(t *testing.T) {
	t.Parallel()

	m, err := sc.New("test", func(b *xparBuilder) {
		b.Initial("test")
		b.Parallel("test", func(b *xparBuilder) {
			b.State("foo", func(b *xparBuilder) {
				b.Initial("foobar")
				b.State("foobar").On[xparGotoFoobaz]().To("foobaz")
				b.State("foobaz").Entry("enteredFoobaz", xparNoop).
					On[xparGotoFoobaz]().To("foobaz").Reenter()
			})
			b.State("bar")
		})
	})
	require.NoError(t, err)

	snap, _ := m.Start(xparCtx{}, epoch)
	snap, first := m.Transition(snap, xparGotoFoobaz{}, epoch)
	_, second := m.Transition(snap, xparGotoFoobaz{}, epoch)

	require.Equal(t, 2, xparCount(first, "foobaz/enteredFoobaz")+xparCount(second, "foobaz/enteredFoobaz"))
}

// XState: parallel states > should raise a "xstate.done.state.*" event when all child states reach final state
func TestXParShouldRaiseADoneStateEventWhenAllChildStatesReachFinalState(t *testing.T) {
	t.Parallel()

	m, err := sc.New("test", func(b *xparBuilder) {
		b.Initial("p")
		b.Parallel("p", func(b *xparBuilder) {
			for _, region := range []string{"a", "b", "c"} {
				b.State(region, func(b *xparBuilder) {
					b.Initial(region + ".idle")
					b.State(region + ".idle").On[xparFinish]().To(region + ".finished")
					b.Final(region + ".finished")
				})
			}
		}).OnDone().To("success")
		b.Final("success")
	})
	require.NoError(t, err)

	snap, _ := m.Start(xparCtx{}, epoch)
	snap, _ = m.Transition(snap, xparFinish{}, epoch)

	// XState waits for the actor to complete.
	require.True(t, snap.Done())
	require.Equal(t, []string{"success"}, snap.Active())
}

// XState: parallel states > should raise a "xstate.done.state.*" event when a pseudostate of a history type is directly on a parallel state
//
// XState gives the history state two default targets, one per region. An sc
// history state has one default, and without one it restores the parent's
// default entry, which for a parallel state is both regions.
func TestXParShouldRaiseADoneStateEventWhenAHistoryStateIsDirectlyOnAParallelState(t *testing.T) {
	t.Parallel()

	m, err := sc.New("historyOnParallel", func(b *xparBuilder) {
		b.Initial("parallelSteps")
		b.Parallel("parallelSteps", func(b *xparBuilder) {
			b.History("hist", sc.Shallow)
			b.State("one", func(b *xparBuilder) {
				b.Initial("wait_one")
				b.State("wait_one").On[xparFinishOne]().To("one.done")
				b.Final("one.done")
			})
			b.State("two", func(b *xparBuilder) {
				b.Initial("wait_two")
				b.State("wait_two").On[xparFinishTwo]().To("two.done")
				b.Final("two.done")
			})
		}).OnDone().To("finished")
		b.State("finished")
	})
	require.NoError(t, err)

	snap, _ := m.Start(xparCtx{}, epoch)
	snap, _ = m.Transition(snap, xparFinishOne{}, epoch)
	snap, _ = m.Transition(snap, xparFinishTwo{}, epoch)

	require.Equal(t, []string{"finished"}, snap.Active())
}

// XState: parallel states > source parallel region should not be exited when a transition within it targets another parallel region (parallel root)
func TestXParSourceRegionShouldNotBeExitedWhenTargetingAnotherRegionParallelRoot(t *testing.T) {
	t.Parallel()

	m, err := sc.New("crossRegionRoot", func(b *xparBuilder) {
		b.Initial("root")
		xparTracked(b.Parallel("root", func(b *xparBuilder) {
			xparTracked(b.State("Operation", func(b *xparBuilder) {
				b.Initial("Waiting")
				xparTracked(b.State("Waiting")).On[xparToggleMode]().To("Demo")
				xparTracked(b.State("Fetching"))
			}))
			xparTracked(b.State("Mode", func(b *xparBuilder) {
				b.Initial("Normal")
				xparTracked(b.State("Normal"))
				xparTracked(b.State("Demo"))
			}))
		}))
	}, sc.AllowUnreachable())
	require.NoError(t, err)

	snap, _ := m.Start(xparCtx{}, epoch)
	_, effects := m.Transition(snap, xparToggleMode{}, epoch)

	require.Equal(t, []string{"exit: Normal", "enter: Demo"}, xparTrace(effects))
}

// XState: parallel states > source parallel region should not be exited when a transition within it targets another parallel region (nested parallel)
func TestXParSourceRegionShouldNotBeExitedWhenTargetingAnotherRegionNestedParallel(t *testing.T) {
	t.Parallel()

	m, err := sc.New("crossRegionNested", func(b *xparBuilder) {
		b.Initial("a")
		xparTracked(b.Parallel("a", func(b *xparBuilder) {
			xparTracked(b.State("Operation", func(b *xparBuilder) {
				b.Initial("Waiting")
				xparTracked(b.State("Waiting")).On[xparToggleMode]().To("Demo")
				xparTracked(b.State("Fetching"))
			}))
			xparTracked(b.State("Mode", func(b *xparBuilder) {
				b.Initial("Normal")
				xparTracked(b.State("Normal"))
				xparTracked(b.State("Demo"))
			}))
		}))
	}, sc.AllowUnreachable())
	require.NoError(t, err)

	snap, _ := m.Start(xparCtx{}, epoch)
	_, effects := m.Transition(snap, xparToggleMode{}, epoch)

	require.Equal(t, []string{"exit: Normal", "enter: Demo"}, xparTrace(effects))
}

// XState: parallel states > targetless transition on a parallel state should not enter nor exit any states
func TestXParTargetlessTransitionOnAParallelStateShouldNotEnterNorExitAnyStates(t *testing.T) {
	t.Parallel()

	m, err := sc.New("test", func(b *xparBuilder) {
		b.Initial("test")
		xparTracked(b.Parallel("test", func(b *xparBuilder) {
			xparTracked(b.State("first", func(b *xparBuilder) {
				b.Initial("disabled")
				xparTracked(b.State("disabled"))
				xparTracked(b.State("enabled"))
			}))
			xparTracked(b.State("second"))
		})).On[xparMyEvent]().Do("act", xparDo[xparMyEvent])
	}, sc.AllowUnreachable())
	require.NoError(t, err)

	snap, _ := m.Start(xparCtx{}, epoch)
	_, effects := m.Transition(snap, xparMyEvent{}, epoch)

	require.Empty(t, xparTrace(effects))
	// Not asserted by XState: the transition's own action ran.
	require.Equal(t, []string{"test/act"}, names(effects))
}

// XState: parallel states > targetless transition in one of the parallel regions should not enter nor exit any states
func TestXParTargetlessTransitionInOneOfTheRegionsShouldNotEnterNorExitAnyStates(t *testing.T) {
	t.Parallel()

	m, err := sc.New("test", func(b *xparBuilder) {
		b.Initial("test")
		xparTracked(b.Parallel("test", func(b *xparBuilder) {
			xparTracked(b.State("first", func(b *xparBuilder) {
				b.Initial("disabled")
				xparTracked(b.State("disabled"))
				xparTracked(b.State("enabled"))
			})).On[xparMyEvent]().Do("act", xparDo[xparMyEvent])
			xparTracked(b.State("second"))
		}))
	}, sc.AllowUnreachable())
	require.NoError(t, err)

	snap, _ := m.Start(xparCtx{}, epoch)
	_, effects := m.Transition(snap, xparMyEvent{}, epoch)

	require.Empty(t, xparTrace(effects))
	// Not asserted by XState: the transition's own action ran.
	require.Equal(t, []string{"first/act"}, names(effects))
}

// xparFinishingRegion declares a region that waits in name.active and
// finishes in name.done on an event of type Ev.
func xparFinishingRegion[Ev any](b *xparBuilder, name string) {
	b.State(name, func(b *xparBuilder) {
		b.Initial(name + ".active")
		b.State(name + ".active").On[Ev]().To(name + ".done")
		b.Final(name + ".done")
	})
}

// XState: parallel onDone output aggregation > should aggregate region outputs into a keyed object
//
// Partial port: sc final states have no output, so only the parallel state
// completing and its onDone transition being taken are checked.
func TestXParOutputShouldAggregateRegionOutputsIntoAKeyedObject(t *testing.T) {
	t.Parallel()

	m, err := sc.New("aggregate", func(b *xparBuilder) {
		b.Initial("processing")
		b.Parallel("processing", func(b *xparBuilder) {
			xparFinishingRegion[xparUploaded](b, "upload")
			xparFinishingRegion[xparValid](b, "validate")
		}).OnDone().Do("outputSpy", xparDo[struct{}]).To("success")
		b.Final("success")
	})
	require.NoError(t, err)

	snap, _ := m.Start(xparCtx{}, epoch)
	snap, effects := m.Transition(snap, xparUploaded{}, epoch)
	require.Zero(t, xparCount(effects, "processing/outputSpy"))

	snap, effects = m.Transition(snap, xparValid{}, epoch)

	require.Equal(t, []string{"success"}, snap.Active())
	require.Equal(t, 1, xparCount(effects, "processing/outputSpy"))
}

// XState: parallel onDone output aggregation > should include undefined for regions without output
//
// Partial port: only the onDone transition being taken once is checked.
func TestXParOutputShouldIncludeUndefinedForRegionsWithoutOutput(t *testing.T) {
	t.Parallel()

	m, err := sc.New("partialOutput", func(b *xparBuilder) {
		b.Initial("processing")
		b.Parallel("processing", func(b *xparBuilder) {
			xparFinishingRegion[xparDoneA](b, "withOutput")
			xparFinishingRegion[xparDoneB](b, "withoutOutput")
		}).OnDone().Do("outputSpy", xparDo[struct{}]).To("success")
		b.Final("success")
	})
	require.NoError(t, err)

	snap, _ := m.Start(xparCtx{}, epoch)
	snap, first := m.Transition(snap, xparDoneA{}, epoch)
	_, second := m.Transition(snap, xparDoneB{}, epoch)

	require.Equal(t, 1, xparCount(first, "processing/outputSpy")+xparCount(second, "processing/outputSpy"))
}

// XState: parallel onDone output aggregation > should resolve dynamic output functions before aggregation
//
// Partial port: only the onDone transition being taken once, when one event
// finishes both regions, is checked.
func TestXParOutputShouldResolveDynamicOutputFunctionsBeforeAggregation(t *testing.T) {
	t.Parallel()

	m, err := sc.New("dynamicOutput", func(b *xparBuilder) {
		b.Initial("processing")
		b.Parallel("processing", func(b *xparBuilder) {
			xparFinishingRegion[xparDone](b, "a")
			xparFinishingRegion[xparDone](b, "b")
		}).OnDone().Do("outputSpy", xparDo[struct{}]).To("success")
		b.Final("success")
	})
	require.NoError(t, err)

	snap, _ := m.Start(xparCtx{}, epoch)
	_, effects := m.Transition(snap, xparDone{}, epoch)

	require.Equal(t, 1, xparCount(effects, "processing/outputSpy"))
}

// XState: parallel onDone output aggregation > should aggregate nested parallel outputs
//
// Partial port: only the onDone transition of the outer parallel state being
// taken once, when one event finishes every nested region, is checked.
func TestXParOutputShouldAggregateNestedParallelOutputs(t *testing.T) {
	t.Parallel()

	m, err := sc.New("nestedOutput", func(b *xparBuilder) {
		b.Initial("outer")
		b.Parallel("outer", func(b *xparBuilder) {
			xparFinishingRegion[xparDone](b, "branch1")
			b.Parallel("branch2", func(b *xparBuilder) {
				xparFinishingRegion[xparDone](b, "inner1")
				xparFinishingRegion[xparDone](b, "inner2")
			})
		}).OnDone().Do("outputSpy", xparDo[struct{}]).To("success")
		b.Final("success")
	})
	require.NoError(t, err)

	snap, _ := m.Start(xparCtx{}, epoch)
	snap, effects := m.Transition(snap, xparDone{}, epoch)

	require.Equal(t, 1, xparCount(effects, "outer/outputSpy"))
	require.Equal(t, []string{"success"}, snap.Active())
}

// XState: parallel onDone output aggregation > should provide aggregated output for root parallel machine
//
// Partial port: sc has no machine output, and its root is not parallel, so
// the wrapping parallel state needs an onDone transition to a top-level final
// state for the machine to complete. Only that completion is checked.
func TestXParOutputShouldProvideAggregatedOutputForRootParallelMachine(t *testing.T) {
	t.Parallel()

	m, err := sc.New("rootOutput", func(b *xparBuilder) {
		b.Initial("root")
		b.Parallel("root", func(b *xparBuilder) {
			xparFinishingRegion[xparDone](b, "a")
			xparFinishingRegion[xparDone](b, "b")
		}).OnDone().To("completed")
		b.Final("completed")
	})
	require.NoError(t, err)

	snap, _ := m.Start(xparCtx{}, epoch)
	require.False(t, snap.Done())

	snap, _ = m.Transition(snap, xparDone{}, epoch)

	require.True(t, snap.Done())
}

// XState: parallel state conformance > a transition targeting one region should not reset sibling regions
func TestXParConformanceATransitionTargetingOneRegionShouldNotResetSiblingRegions(t *testing.T) {
	t.Parallel()

	m, err := sc.New("p", func(b *xparBuilder) {
		b.Initial("p")
		p := b.Parallel("p", func(b *xparBuilder) {
			b.State("phase", func(b *xparBuilder) {
				b.Initial("inquiry")
				b.State("inquiry")
				b.State("archive")
			})
			b.State("mode", func(b *xparBuilder) {
				b.Initial("new")
				b.State("new")
				b.State("edit")
			})
		})
		p.On[xparArchive]().To("archive")
		p.On[xparEdit]().To("edit")
	})
	require.NoError(t, err)

	snap, _ := m.Start(xparCtx{}, epoch)
	snap, _ = m.Transition(snap, xparEdit{}, epoch)
	snap, _ = m.Transition(snap, xparArchive{}, epoch)

	require.Equal(t, []string{"p", "phase", "archive", "mode", "edit"}, snap.Active())
}

// XState: parallel state conformance > reenter in one region should not re-run sibling region entry actions
//
// XState's sibling entry action increments a count in the context. An sc
// entry action cannot assign, so the sibling's entries are counted from the
// effects.
func TestXParConformanceReenterInOneRegionShouldNotRerunSiblingRegionEntryActions(t *testing.T) {
	t.Parallel()

	m, err := sc.New("reentrytest", func(b *xparBuilder) {
		b.Initial("reentrytest")
		b.Parallel("reentrytest", func(b *xparBuilder) {
			b.State("someStateWithReentry", func(b *xparBuilder) {
				b.Initial("a")
				b.State("a")
				b.State("b")
			}).On[xparReenterA]().To("a").Reenter()
			b.State("sibling", func(b *xparBuilder) {
				b.Initial("c")
				b.State("c")
				b.State("d")
			}).Entry("count", xparNoop)
		}).Entry("reentered", xparNoop) // not in XState: shows how far Reenter reaches
	}, sc.AllowUnreachable())
	require.NoError(t, err)

	snap, effects := m.Start(xparCtx{}, epoch)
	require.Equal(t, 1, xparCount(effects, "sibling/count"))

	_, effects = m.Transition(snap, xparReenterA{}, epoch)

	require.Zero(t, xparCount(effects, "sibling/count"))
	// Not asserted by XState: the parallel state is not re-entered either.
	require.Zero(t, xparCount(effects, "reentrytest/reentered"))
}

// XState: parallel state conformance > sibling region events should still be handled after a cross-region transition
func TestXParConformanceSiblingRegionEventsShouldStillBeHandledAfterACrossRegionTransition(t *testing.T) {
	t.Parallel()

	m, err := sc.New("question-flow", func(b *xparBuilder) {
		b.Initial("question-flow")
		b.Parallel("question-flow", func(b *xparBuilder) {
			b.State("value1", func(b *xparBuilder) {
				b.Initial("x")
				b.State("x").On[xparNext]().To("value2.shown")
				b.Final("value1.done")
			})
			b.State("value2", func(b *xparBuilder) {
				b.Initial("value2.hidden")
				b.State("value2.shown").On[xparNext]().To("value3.shown")
				b.State("value2.hidden")
			})
			b.State("value3", func(b *xparBuilder) {
				b.Initial("value3.hidden")
				b.State("value3.hidden")
				b.Final("value3.shown")
			})
		})
	}, sc.AllowUnreachable())
	require.NoError(t, err)

	snap, _ := m.Start(xparCtx{}, epoch)

	snap, _ = m.Transition(snap, xparNext{}, epoch)
	require.Equal(t,
		[]string{"question-flow", "value1", "x", "value2", "value2.shown", "value3", "value3.hidden"},
		snap.Active(),
	)

	snap, _ = m.Transition(snap, xparNext{}, epoch)
	require.Equal(t,
		[]string{"question-flow", "value1", "x", "value2", "value2.shown", "value3", "value3.shown"},
		snap.Active(),
	)
}

// XState: parallel state conformance > a final region under a parallel root should ignore events it does not handle
func TestXParConformanceAFinalRegionUnderAParallelRootShouldIgnoreEventsItDoesNotHandle(t *testing.T) {
	t.Parallel()

	_, err := sc.New("g", func(b *xparBuilder) {
		b.Initial("g")
		b.Parallel("g", func(b *xparBuilder) {
			b.Final("regionA")
			b.State("regionB", func(b *xparBuilder) {
				b.Initial("idle")
				b.State("idle")
				b.State("other")
			})
		})
	}, sc.AllowUnreachable())

	// DIVERGES from XState: XState accepts a final state as a region of a
	// parallel state; the machine starts in {regionA: {}, regionB: 'idle'}
	// and an event nothing handles leaves it there. sc rejects the chart
	// when it is declared, so there is nothing to send an event to.
	require.ErrorContains(t, err, `final state "regionA": declared directly inside parallel state "g"; put it inside a region`)
}
