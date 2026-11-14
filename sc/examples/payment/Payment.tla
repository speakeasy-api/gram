------------------------------ MODULE Payment ------------------------------
(* An order that charges a card, retries declined charges after a delay,   *)
(* and can be cancelled. The spec models the chart together with the       *)
(* driver that carries out its effects and the payment provider, because   *)
(* the rules below are about what happens to the customer's card, which    *)
(* the chart alone cannot see.                                             *)
(*                                                                         *)
(* The two BOOLEAN constants each switch one design decision. Payment.cfg  *)
(* sets both to TRUE, the design the chart implements. Setting either to   *)
(* FALSE in a copy of the cfg reproduces the trace that ruled the other    *)
(* design out.                                                             *)
EXTENDS Naturals, FiniteSets

CONSTANTS
  MaxAttempts,      \* declined charges that are retried before giving up
  MaxLost,          \* answers the provider may lose; bounds the model
  CancelWaits,      \* TRUE: a cancel waits for a charge that may have landed
  UnknownKeepsKey   \* TRUE: a charge with an unknown outcome is asked again
                    \*       under the same idempotency key

VARIABLES
  state,            \* chart: the active state
  attempts,         \* context: declined charges so far
  cancelRequested,  \* context: the customer cancelled; waiting to learn
                    \*          whether the card was charged
  unresolved,       \* context: the last charge ended without a clear answer
  run,              \* chart: the service run it is waiting for; 0 = none
  timer,            \* chart: TRUE while the retry timer is pending
  runs,             \* driver: service runs started so far
  jobs,             \* driver: charge runs still in the outbox
  results,          \* driver: answers on their way to the chart
  outcome,          \* provider: what each idempotency key came to
  lost,             \* provider: answers lost so far
  refunds           \* driver: idempotency keys a refund was requested for

vars == <<state, attempts, cancelRequested, unresolved, run, timer,
          runs, jobs, results, outcome, lost, refunds>>

\* The idempotency key of a charge is the attempt number, so every repeat
\* of one attempt is the same request to the provider.
Keys == 0..MaxAttempts
RunIds == 1..(MaxAttempts + 1 + MaxLost)
Jobs == [id : RunIds, key : Keys]
Answers == {"charged", "declined", "unknown"}

TypeOK ==
  /\ state \in {"idle", "charging", "retrying", "paid", "cancelled"}
  /\ attempts \in Keys
  /\ cancelRequested \in BOOLEAN
  /\ unresolved \in BOOLEAN
  /\ run \in {0} \cup RunIds
  /\ timer \in BOOLEAN
  /\ runs \in 0..(MaxAttempts + 1 + MaxLost)
  /\ jobs \subseteq Jobs
  /\ results \subseteq [run : RunIds, answer : Answers]
  /\ outcome \in [Keys -> {"none", "charged", "declined"}]
  /\ lost \in 0..MaxLost
  /\ refunds \subseteq Keys

Init ==
  /\ state = "idle"
  /\ attempts = 0
  /\ cancelRequested = FALSE
  /\ unresolved = FALSE
  /\ run = 0
  /\ timer = FALSE
  /\ runs = 0
  /\ jobs = {}
  /\ results = {}
  /\ outcome = [k \in Keys |-> "none"]
  /\ lost = 0
  /\ refunds = {}

----------------------------------------------------------------------------
(* The chart. Each action is one transition.                               *)

\* Entering charging starts the charge service: a run joins the outbox,
\* carrying the idempotency key of the attempt it belongs to.
StartCharge(key) ==
  /\ state' = "charging"
  /\ runs' = runs + 1
  /\ run' = runs + 1
  /\ jobs' = jobs \cup {[id |-> runs + 1, key |-> key]}

\* The chart only accepts an answer to the run it is waiting for.
Answered(answer) == [run |-> run, answer |-> answer] \in results

Submit ==
  /\ state = "idle"
  /\ StartCharge(attempts)
  /\ UNCHANGED <<attempts, cancelRequested, unresolved, timer, results, outcome, lost, refunds>>

ChargeSucceeded ==
  /\ state = "charging"
  /\ Answered("charged")
  /\ run' = 0
  /\ unresolved' = FALSE
  /\ IF cancelRequested
       THEN state' = "cancelled" /\ refunds' = refunds \cup {attempts}
       ELSE state' = "paid" /\ UNCHANGED refunds
  /\ UNCHANGED <<attempts, cancelRequested, timer, runs, jobs, results, outcome, lost>>

\* A decline is a clear answer: this attempt took no money. The next
\* attempt is a new request with a new key.
Declined ==
  /\ run' = 0
  /\ unresolved' = FALSE
  /\ IF ~cancelRequested /\ attempts < MaxAttempts
       THEN state' = "retrying" /\ timer' = TRUE /\ attempts' = attempts + 1
       ELSE state' = "cancelled" /\ UNCHANGED <<timer, attempts>>

ChargeDeclined ==
  /\ state = "charging"
  /\ Answered("declined")
  /\ Declined
  /\ UNCHANGED <<cancelRequested, runs, jobs, results, outcome, lost, refunds>>

\* A timeout or a broken connection says nothing about the card. The chart
\* asks again under the same key until the provider gives a clear answer.
ChargeUnknown ==
  /\ state = "charging"
  /\ Answered("unknown")
  /\ IF UnknownKeepsKey
       THEN /\ run' = 0
            /\ unresolved' = TRUE
            /\ state' = "retrying"
            /\ timer' = TRUE
            /\ UNCHANGED attempts
       ELSE Declined
  /\ UNCHANGED <<cancelRequested, runs, jobs, results, outcome, lost, refunds>>

RetryTimerFired ==
  /\ state = "retrying"
  /\ timer
  /\ timer' = FALSE
  /\ StartCharge(attempts)
  /\ UNCHANGED <<attempts, cancelRequested, unresolved, results, outcome, lost, refunds>>

\* Cancelling is immediate only when no charge can still land: between
\* attempts, after a clear decline. Otherwise the chart records the request
\* and keeps going until it knows whether there is a charge to refund.
Cancel ==
  /\ state \in {"charging", "retrying"}
  /\ ~cancelRequested
  /\ IF CancelWaits /\ (state = "charging" \/ unresolved)
       THEN cancelRequested' = TRUE /\ UNCHANGED <<state, run, timer>>
       ELSE state' = "cancelled" /\ run' = 0 /\ timer' = FALSE /\ UNCHANGED cancelRequested
  /\ UNCHANGED <<attempts, unresolved, runs, jobs, results, outcome, lost, refunds>>

----------------------------------------------------------------------------
(* The driver and the payment provider.                                    *)

\* A worker carries out a charge run. Afterwards it either acknowledges
\* the run, which removes it from the outbox, or stops before it can, which
\* leaves the run to be carried out again. The provider decides what a key
\* comes to the first time it sees the key and repeats that afterwards. Its
\* answer can be lost on the way back, in which case the worker reports
\* only that it does not know.
WorkerRuns(j) ==
  /\ j \in jobs
  /\ jobs' \in {jobs, jobs \ {j}}
  /\ \E first \in {"charged", "declined"} :
       LET final == IF outcome[j.key] = "none" THEN first ELSE outcome[j.key] IN
         /\ outcome' = [outcome EXCEPT ![j.key] = final]
         /\ \/ /\ results' = results \cup {[run |-> j.id, answer |-> final]}
               /\ UNCHANGED lost
            \/ /\ lost < MaxLost
               /\ lost' = lost + 1
               /\ results' = results \cup {[run |-> j.id, answer |-> "unknown"]}
  /\ UNCHANGED <<state, attempts, cancelRequested, unresolved, run, timer, runs, refunds>>

\* Leaving the state that invoked a service stops it. The driver removes
\* the run from the outbox unless a worker already holds it, so a stop can
\* prevent the run or lose the race with it.
StopDropsJob(j) ==
  /\ j \in jobs
  /\ j.id # run
  /\ jobs' = jobs \ {j}
  /\ UNCHANGED <<state, attempts, cancelRequested, unresolved, run, timer, runs, results, outcome, lost, refunds>>

\* Final states have no way out. This step keeps TLC from reporting them
\* as deadlocks.
Finished ==
  /\ state \in {"paid", "cancelled"}
  /\ UNCHANGED vars

Next ==
  \/ Submit
  \/ ChargeSucceeded
  \/ ChargeDeclined
  \/ ChargeUnknown
  \/ RetryTimerFired
  \/ Cancel
  \/ \E j \in Jobs : WorkerRuns(j)
  \/ \E j \in Jobs : StopDropsJob(j)
  \/ Finished

\* The chart reacts to answers and timers, and workers do run outbox jobs.
\* Nothing forces a customer to submit or cancel.
Fairness ==
  /\ WF_vars(ChargeSucceeded)
  /\ WF_vars(ChargeDeclined)
  /\ WF_vars(ChargeUnknown)
  /\ WF_vars(RetryTimerFired)
  /\ \A j \in Jobs : WF_vars(WorkerRuns(j))

Spec == Init /\ [][Next]_vars /\ Fairness

----------------------------------------------------------------------------
(* The rules.                                                              *)

Charged == {k \in Keys : outcome[k] = "charged"}

\* The card is charged at most once per order.
ChargedAtMostOnce == Cardinality(Charged) <= 1

\* An order is only paid if the card was charged.
PaidMeansCharged == state = "paid" => Charged # {}

\* A cancelled order never keeps the customer's money: a refund has been
\* requested for every charge. Carrying the request out is the provider's
\* side, which the model leaves to the refund being durable and idempotent.
NoChargeKeptAfterCancel == state = "cancelled" => Charged \subseteq refunds

\* Once submitted, an order ends up paid or cancelled.
Settles == (state # "idle") ~> (state \in {"paid", "cancelled"})
=============================================================================
