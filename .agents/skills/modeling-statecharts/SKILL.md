---
name: modeling-statecharts
description: Use when designing, changing, or reviewing a stateful workflow built on the `sc` statechart package (`sc/`, `sc/pgdriver`) — order, payment, approval, provisioning, retry, timeout, or cancellation flows, or anything called a state machine, lifecycle, or statechart. Also use for "model this in TLA+", "write a spec", "what are the invariants", "can this get stuck, double-charge, or lose an event", `mise run tlc`, a TLC counterexample trace, `.tla` or `.cfg` files, or `Machine.Explore` invariants.
---

# Modeling statecharts

Agree the rules, check them against a TLA+ model with TLC, then build the chart with `sc` and check the chart's share of each rule with `Machine.Explore`.

Core principle: **the chart is half the system.** Its effects run later and possibly twice, an external call can take effect and still report failure, and answers arrive at a chart that may have moved on. So the model covers the chart, the driver that carries out its effects, and the outside world those effects change. A model of the chart alone finds nothing `Explore` would not find faster.

Start from the worked example and keep its structure: `sc/examples/payment/` has `Payment.tla` with `Payment.cfg` and `PaymentLarge.cfg` (the model at two sizes), `payment.go` (the chart) and `payment_test.go` (the checks).

## Workflow

| Step                       | Output                                                                                                                                                                                                                                                                                                               |
| -------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1. Rules                   | Plain sentences in the user's words. "Never" rules become invariants and "eventually" rules become properties. A rule that can only be said about a step ("registration never starts once an abort was accepted") becomes an action property `[][...]_vars` under `PROPERTIES`; prefer an invariant when one will do |
| 2. Model                   | `<Chart>.tla`, `<Chart>.cfg` and `<Chart>Large.cfg` in the Go package that will hold the chart                                                                                                                                                                                                                       |
| 3. Check                   | `mise run tlc` passes on the design with both cfgs                                                                                                                                                                                                                                                                   |
| 4. Reject the alternatives | One constant per simpler design you ruled out, and a trace from TLC for each                                                                                                                                                                                                                                         |
| 5. Build                   | The `sc` chart                                                                                                                                                                                                                                                                                                       |
| 6. Check the chart         | An `Explore` test, and one replay test per trace                                                                                                                                                                                                                                                                     |
| 7. Hand off                | See below                                                                                                                                                                                                                                                                                                            |

The spec and cfg are committed next to the chart. CI does not run them, so when you change a chart, change its spec and rerun TLC in the same change.

## Running TLC

Run from the repo root, where the task is defined, with `-metadir` pointed at a scratch directory so TLC does not write `states/` into the repo:

```bash
pkg=sc/examples/payment
mise run tlc -metadir "$(mktemp -d)" -config $pkg/Payment.cfg $pkg/Payment.tla
```

- `Model checking completed. No error has been found.` means every rule in the cfg holds.
- A violation prints `Error: Invariant <Name> is violated.` and the shortest trace, each state labelled with its action. The task then ends with `[tlc] ERROR task failed`, which only reflects TLC's exit code.
- To check a rejected design, copy the cfg to a scratch directory, flip its constant, and pass the copy to `-config`. TLC stops at the first broken rule. Removing that rule from the copy shows whether another also breaks, which is worth doing when the first trace is not the one you expected.

## Writing the model

- Write `Next` as a disjunction of named actions, one per chart transition. Quantify over constant sets (`\E j \in Jobs : WorkerRuns(j)`, with `j \in jobs` inside the action): over a variable, TLC labels the step `Next`.
- Include `TypeOK`, and a step that leaves everything unchanged in final states (`Finished`), or TLC reports them as deadlocks.
- Put fairness only on what the system does by itself: reacting to answers and timers, workers running jobs. Never on what a user chooses.
- **Keep each rejected design behind a constant**, as `CancelWaits` and `UnknownKeepsKey` do. A good first design often passes at once; the constants are where the traces for steps 4, 6 and 7 come from, and they show a rule is not passing vacuously.
- When a rule fails on the design, change the design. If the rule was wrong, take the corrected rule to the user; do not weaken it to get a pass.

### Bounds are part of the claim

TLC proves the rules only for the constants in the cfg, so choose them for what they mean, not only for speed.

- **Fault budgets** (lost answers, crashes, failed deletes) limit the claim: `MaxLost = 1` proves nothing about two lost answers. Liveness needs such a budget to hold at all, which makes it an assumption about the world to report.
- **Sizes** (versions, reviewers, attempts) mostly need 2 to expose an interaction, sometimes 3.
- **Bookkeeping ranges**, such as run IDs, exclude nothing.

Commit two cfgs. `<Chart>.cfg` is the everyday check: seconds, numeric constants of 1 or 2, matching the Go constants where it can. `<Chart>Large.cfg` raises the fault budgets and sizes as far as stays under about a minute, and is run before the design is handed off. Say in each cfg what it covers. The payment model takes 1 s at 2 declines and 1 lost answer, 26 s at 3 and 2, and minutes and 5 GB at 3 and 3.

If TLC runs for minutes at small constants, the cause is usually a set that only grows. Drop an answer the chart can no longer accept instead of keeping it, and stop outside actors acting once the chart cannot hear them.

### What `sc` does, and how to model it

| Behaviour                                                                                                                                            | Model it as                                                                                                                                                                                                           |
| ---------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| A chart processes one event at a time; a transition, its assignments and the recording of its effects are atomic                                     | one action per transition                                                                                                                                                                                             |
| Entering a state with `Invoke` records a service run; a worker runs it later, possibly more than once                                                | a job in an outbox set; the worker action may leave it there. A chart whose initial state invokes has its first job in `Init`                                                                                         |
| A result is accepted only if it belongs to the run the chart is waiting for                                                                          | a `run` variable compared in the action                                                                                                                                                                               |
| Leaving the invoking state stops the service, but the stop can lose the race                                                                         | the job may be dropped, or may still run                                                                                                                                                                              |
| An external call can take effect and report failure, or never answer                                                                                 | the worker action picks "took effect" and "what the chart is told" separately, with a bound on lost answers                                                                                                           |
| An `After` timer fires once, is cancelled on exit, and a late firing is ignored. Its transition may be targetless, which is how to act once and stay | a boolean cleared on exit and on firing                                                                                                                                                                               |
| An event from a person or another system can arrive late, twice, or about something that has since been replaced                                     | a set of sent messages the chart action reads from. Put the identifier of what was decided on the event                                                                                                               |
| Actions (`Do`, `Entry`, `Exit`) cannot fail and run after the transition is saved, possibly twice                                                    | a set of requested effects. If a rule depends on the action having run, give it a worker action too                                                                                                                   |
| Parallel regions                                                                                                                                     | one variable or function entry per region. The parallel state's `OnDone` fires in the same step as the last region finishing. In the chart, mirror each region in a context field that guards and invariants can read |
| A step retried until it succeeds (a wait state with `After` back to the invoking state, as `Retrying` is)                                            | no counter in the context; a driver-side run counter, and a bound on failures in the outside world so liveness can hold. Report "retries forever" as an assumption                                                    |

### Idempotency and leaving states

These two decisions cause most of the traces. Model both sides and let TLC decide.

- **Every repeat of one request shares one idempotency key**, derived from the context because that is all a service receives. Repeats include a worker running the job twice and a retry after an answer that might hide a success, such as a timeout. Only a definite refusal may start a new key. The payment chart's key is order ID plus the count of declines for this reason. A request that is never retried after a refusal has one key, the instance's ID, and needs no key set in the model.
- **"The outside world decides once per key" is an assumption about someone else's API.** Model it, add a constant that turns it off, and write what fails as a contract in the doc comment of the Go interface.
- **A state whose service's answer still matters has two safe exits.** Leaving ignores the answer, so either stay (record the request in the context with a targetless transition and decide when the answer arrives, as `Cancel` does), or leave at once and compensate. Compensating is only safe if it also makes the outside world refuse a late arrival of the original request, which is a contract to put behind a constant. Choose per state; one chart can use both.

## Building the chart

Read the API with `mise exec -- go doc -all ./sc`. Ignore a leading `invalid version: unknown revision` line.

| In the model                                   | In `sc`                                                                                                                                                                                                                        |
| ---------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| A value of the `state` variable                | a state constant; `b.Final` for one with only the `Finished` step                                                                                                                                                              |
| Any other chart variable                       | a context field                                                                                                                                                                                                                |
| A chart action                                 | one transition: `On[Ev]`, `After`, `Invocation.OnDone`, `Invocation.OnError`                                                                                                                                                   |
| An action's condition, or its `IF`             | `When`; for an `IF`, two transitions for the same trigger with the guarded one first. Guards see only the context and the event, never the active states, so anything a guard or an invariant needs to know is a context field |
| Primed chart variables                         | `Assign`, and `To` for `state'`                                                                                                                                                                                                |
| A change to a driver or outside-world variable | the service or a `Do` action; nothing in the context                                                                                                                                                                           |
| Driver and outside-world actions               | nothing: drivers do this. In `Explore` they are `Outcomes`                                                                                                                                                                     |

## Checking the chart

`Explore` walks every state the real chart can reach and returns the shortest trace to anything that breaks a rule. The chart cannot see the outside world, so translate each rule into the chart's share of it:

| Model rule                       | `Explore` check                                                                                                                                                                                                                                         |
| -------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Invariant over chart variables   | `Invariant` on the snapshot                                                                                                                                                                                                                             |
| Invariant over the outside world | `StepInvariant` on what the chart asks for. A started service is an effect with `Kind == sc.EffectStartInvocation`, its `Name`, and the `Context` it will run with, so a check can read the idempotency key. A `Do` action is an effect with its `Name` |
| Liveness                         | `report.Stuck` is empty and `report.Reached` holds for every state                                                                                                                                                                                      |

- Assert `report.Truncated` is false. The context must take finitely many values: cap a counter with a guard, or carry the identifier on the event, which the finite `Events` list bounds.
- Give `sc.Succeeds` the service's exact result type. An outcome of another type matches no transition and shows up as a stuck state.
- Step checks run for every event tried, including ones no state handles. When several services share a result type, tell them apart with `step.From.In(...)`.
- Replay each trace from step 4 as a test. Chart steps are events sent to `Transition`; a worker step becomes the `sc.Done` or `sc.Failed` it would deliver. A worker step after the chart has moved on is ignored by the chart, so to show the outside world's share, run the effects with `Machine.Exec` against a fake that keeps the interface contract. A trace that fails only through outside-world steps has no chart fix: it becomes the interface contract, and the test covers the chart's share, such as two charges carrying one key.

Run `mise run test:sc ./path/to/package/...` (paths relative to `sc/`), adding `-count=1` to bypass cached results. `mise run lint:sc` lints all of `sc/`; for one package run `mise exec -- golangci-lint run ./path/to/package/...` from `sc/`.

## Hand off

1. The rules in the user's words, with the model rule and the test that check each.
2. Every trace found, as numbered steps in plain language, with the design decision it settled.
3. Assumptions you made and rules you added, each as a decision the user can reverse, and every contract the outside world must keep.
4. What the model leaves out, and the bounds each cfg was checked at, in the user's terms ("holds for up to 2 lost answers").
5. Any rule that was changed or dropped, and why.

## Common mistakes

- Modeling only the chart. Every rule passes and nothing was learned.
- Giving each retry a new idempotency key. A timed-out call that did take effect is then repeated as a new request.
- Reporting no traces because the first design passed. Add the rejected designs as constants and run them.
- Reporting "TLC passes" without the bounds. The pass covers only the fault budget in the cfg.
- Reading `[tlc] ERROR task failed` as a broken toolchain. Scroll up: TLC found a violation and printed its trace.
