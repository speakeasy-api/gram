---------------------------- MODULE Certificate ----------------------------
(* How the gateway slots of one private network connection obtain and      *)
(* renew the connection's TLS certificate. Each slot runs the certificate  *)
(* chart in certchart.go; the spec models every slot's chart together with *)
(* what its services do to the outside world: the shared certificate store *)
(* (network_ingress_certificate_states), the issuance lease row in the     *)
(* same table, tsnet, and Let's Encrypt. CERTIFICATES.md explains why.     *)
(*                                                                         *)
(* The rules, in the words of CERTIFICATES.md:                             *)
(*                                                                         *)
(*  - OneNewOrder: the connection places at most one new (non-renewal)     *)
(*    order with Let's Encrypt, ever. A second one spends the tailnet's    *)
(*    50 a week and the name's 5 a week.                                   *)
(*  - OneRenewalPerCycle: each certificate is replaced at most once. An    *)
(*    ARI renewal naming a certificate that was already replaced is not    *)
(*    exempt from the limits: Let's Encrypt counts it as a new order.      *)
(*  - ServedIsIssued: a slot only serves a certificate that was issued and *)
(*    stored.                                                              *)
(*  - Converges: every slot that is not closed comes to hold the latest    *)
(*    stored certificate.                                                  *)
(*  - Renews: the connection gets its first certificate, and renews it     *)
(*    whenever it falls due.                                               *)
(*                                                                         *)
(* Certificates are versions 1, 2, ...: version v + 1 is the one issued    *)
(* after v. Time is abstract. A world step, CyclePasses, makes every       *)
(* certificate stored so far due for renewal; it happens at most MaxCycles *)
(* times, which keeps the model finite.                                    *)
(*                                                                         *)
(* Each BOOLEAN constant below switches on one design that was ruled out,  *)
(* or one contract of the outside world broken. Certificate.cfg sets them  *)
(* all to FALSE, the design certchart.go implements. Setting one to TRUE   *)
(* in a copy of the cfg makes TLC print the trace that ruled it out:       *)
(*                                                                         *)
(*  - NoLease (problem 2 in CERTIFICATES.md): slots order without the      *)
(*    issuance lease. Two slots start with nothing stored, both read the   *)
(*    empty store, both ask tsnet, tsnet finds nothing stored on either,   *)
(*    and both order: two new orders. Breaks OneNewOrder.                  *)
(*  - RenewWithoutReread: Renew takes the lease but does not re-read the   *)
(*    store under it. Two slots hold certificate 1 when it falls due. The  *)
(*    first renews it into 2 and releases the lease; the second, still     *)
(*    holding 1, takes the lease and renews 1 again, which Let's Encrypt   *)
(*    counts as a new order. Breaks OneNewOrder and OneRenewalPerCycle.    *)
(*  - ObtainOnHandshake (problem 3): tsnet terminates TLS and orders a     *)
(*    certificate when a client reaches a slot holding none, outside the   *)
(*    lease. Clients reach two slots before anything is stored; each       *)
(*    slot's tsnet finds the store empty and orders. Breaks OneNewOrder.   *)
(*  - PerSlotRenewal (problem 4): each slot's tsnet renews on its own      *)
(*    schedule, with neither the lease nor a re-read. Two slots holding    *)
(*    certificate 1 both renew it when it falls due: one renewal and one   *)
(*    new order. Breaks OneNewOrder and OneRenewalPerCycle.                *)
(*  - TsnetOrdersBlind (a contract on tsnet): tsnet's CertPair orders even *)
(*    when the store already holds a valid certificate. Obtain reads the   *)
(*    store before it claims the lease and not again after, so two slots   *)
(*    that both read the empty store take the lease in turn and the second *)
(*    orders again although the first has just stored a certificate.       *)
(*    Breaks OneNewOrder. The design relies on tsnet's own store check.    *)
(*  - HolderOutlivesLease (a contract on the lease): a slot still working  *)
(*    under the lease outlives it (a stall longer than the 3 min lease, or *)
(*    a cancelled request whose tsnet order still goes through). The slot  *)
(*    has asked tsnet to order; the lease lapses; another slot takes it,   *)
(*    finds nothing stored and orders too. Breaks OneNewOrder.             *)
(*                                                                         *)
(* What the model assumes of the outside world, each a contract the Go     *)
(* code relies on:                                                         *)
(*                                                                         *)
(*  - Let's Encrypt counts an order naming a certificate that has not been *)
(*    replaced yet as an ARI renewal, exempt from the limits, and counts   *)
(*    any other order as new: one naming nothing or one already replaced.  *)
(*  - tsnet's renewal (CertPairWithValidity) names the held certificate as *)
(*    the one replaced. The model takes the worst case, that tsnet does    *)
(*    not re-read the store first, so Renew's own re-read is what counts.  *)
(*  - tsnet stores what Let's Encrypt issues in the same step: a slot lost *)
(*    between the two would leave an issued certificate nobody stored.     *)
(*  - tsnet's CertPair returns a valid stored certificate instead of       *)
(*    ordering (TsnetOrdersBlind is the breach).                           *)
(*  - A request stopped by Close or its 2 minute timeout stops its tsnet   *)
(*    order, and no live request outlasts its 3 minute lease               *)
(*    (HolderOutlivesLease is the breach).                                 *)
(*  - Failures, and lost lease holders, are bounded (MaxIssuerFaults,      *)
(*    MaxLostHolders): with unbounded failures nothing is ever renewed.    *)
(*                                                                         *)
(* Left out: expiry (a certificate never stops being valid here), the ARI  *)
(* window moving (folded into CyclePasses), repeated window checks, the    *)
(* pod-wide limit of 8 requests, and the 5 s, backoff and rate-limit       *)
(* retry times (a retry is due at some point, nothing more).               *)
EXTENDS Naturals, FiniteSets

CONSTANTS
  Slots,               \* the connection's gateway slots
  MaxCycles,           \* renewal cycles that pass; bounds the model
  MaxIssuerFaults,     \* failed orders and renewals (issuer down, rate
                       \*   limit, tsnet error); bounds the model
  MaxLostHolders,      \* lease holders lost: a slot that crashes holding
                       \*   the lease, which then stays taken until it lapses
  NoLease,             \* TRUE: slots order and renew without the lease
  RenewWithoutReread,  \* TRUE: Renew does not re-read the store under the lease
  ObtainOnHandshake,   \* TRUE: tsnet orders on a handshake to a slot holding none
  PerSlotRenewal,      \* TRUE: each slot's tsnet renews on its own schedule
  TsnetOrdersBlind,    \* TRUE: tsnet's CertPair orders even with a valid stored
                       \*   certificate
  HolderOutlivesLease  \* TRUE: a live lease holder can outlast its lease

VARIABLES
  st,         \* chart, per slot: the active state
  step,       \* driver, per slot: how far the running service has got
  held,       \* context, per slot: the version served; 0 = none
  windowDue,  \* chart, per slot: the window-check timer is pending
  renewSet,   \* context, per slot: the renewal time has been worked out
              \*   (from the ARI window, or two thirds of the lifetime)
  hs,         \* tsnet, per slot: an order started by a client handshake
  lease,      \* store: "free", the slot holding it, or "orphan" for a lease
              \*   left by a lost holder
  stored,     \* store: the latest version stored; 0 = none. Every issued
              \*   certificate is stored, so it is also the latest issued
  dueUpTo,    \* world: versions up to this one are due for renewal
  cycles,     \* world: renewal cycles passed
  newOrders,  \* Let's Encrypt: new (non-renewal) orders
  named,      \* Let's Encrypt: orders naming each version as replaced
  faults,     \* Let's Encrypt and tsnet: failed requests so far
  lost        \* store: lease holders lost so far

slotVars == <<st, step, held, windowDue, renewSet, hs>>
worldVars == <<lease, stored, dueUpTo, cycles, newOrders, named, faults, lost>>
vars == <<slotVars, worldVars>>

\* One version per cycle and the first issuance, plus one more so a
\* rejected design's extra order can be issued and seen.
MaxVersion == MaxCycles + 2
Versions == 1..MaxVersion

States == {"waiting", "obtaining", "retrying", "serving", "checkingStore",
           "fetchingWindow", "renewing", "renewBackoff", "stopped"}
Steps == {"none", "read", "claim", "check", "reread", "issue"}

TypeOK ==
  /\ st \in [Slots -> States]
  /\ step \in [Slots -> Steps]
  /\ held \in [Slots -> 0..MaxVersion]
  /\ windowDue \in [Slots -> BOOLEAN]
  /\ renewSet \in [Slots -> BOOLEAN]
  /\ hs \in [Slots -> {"idle", "issuing"}]
  /\ lease \in {"free", "orphan"} \cup Slots
  /\ stored \in 0..MaxVersion
  /\ dueUpTo \in 0..MaxVersion
  /\ cycles \in 0..MaxCycles
  /\ newOrders \in 0..MaxVersion
  /\ named \in [Versions -> 0..MaxVersion]
  /\ faults \in 0..MaxIssuerFaults
  /\ lost \in 0..MaxLostHolders

Init ==
  /\ st = [s \in Slots |-> "waiting"]
  /\ step = [s \in Slots |-> "none"]
  /\ held = [s \in Slots |-> 0]
  /\ windowDue = [s \in Slots |-> FALSE]
  /\ renewSet = [s \in Slots |-> FALSE]
  /\ hs = [s \in Slots |-> "idle"]
  /\ lease = "free"
  /\ stored = 0
  /\ dueUpTo = 0
  /\ cycles = 0
  /\ newOrders = 0
  /\ named = [v \in Versions |-> 0]
  /\ faults = 0
  /\ lost = 0

----------------------------------------------------------------------------
(* Helpers.                                                                *)

\* Slot s moves to state to with its service at step sp.
Go(s, to, sp) ==
  /\ st' = [st EXCEPT ![s] = to]
  /\ step' = [step EXCEPT ![s] = sp]

\* Slot s holds version v with nothing failing; it asks for the renewal
\* window at once, and has no renewal time until then (serve in
\* certchart.go).
Serve(s, v) ==
  /\ Go(s, "serving", "none")
  /\ held' = [held EXCEPT ![s] = v]
  /\ windowDue' = [windowDue EXCEPT ![s] = TRUE]
  /\ renewSet' = [renewSet EXCEPT ![s] = FALSE]

\* The service's deferred release deletes only the caller's own claim, so a
\* lease that lapsed and was claimed by another slot stays with that slot.
Release(s) == lease' = IF lease = s THEN "free" ELSE lease

\* Let's Encrypt issues the next version and tsnet stores it in the same
\* step. replaced is the version the order names, 0 for none. An order
\* naming a version that has not been replaced yet is an ARI renewal;
\* anything else counts as a new order.
Issue(replaced) ==
  /\ stored < MaxVersion
  /\ stored' = stored + 1
  /\ IF replaced # 0 /\ named[replaced] = 0
       THEN UNCHANGED newOrders
       ELSE newOrders' = newOrders + 1
  /\ named' = IF replaced = 0 THEN named
              ELSE [named EXCEPT ![replaced] = @ + 1]

\* A request to Let's Encrypt or tsnet fails, within the fault budget.
Fails ==
  /\ faults < MaxIssuerFaults
  /\ faults' = faults + 1

----------------------------------------------------------------------------
(* The chart, per slot. Each action is one transition of certchart.go, or  *)
(* one step of the service the active state runs.                          *)

\* The heartbeat's first poll after approval starts the obtain service.
Approved(s) ==
  /\ st[s] = "waiting"
  /\ Go(s, "obtaining", "read")
  /\ UNCHANGED <<held, windowDue, renewSet, hs, worldVars>>

\* Obtain, step 1: reuse a stored certificate without the lease, as every
\* start after the first issuance does.
ObtainReadsStore(s) ==
  /\ st[s] = "obtaining"
  /\ step[s] = "read"
  /\ IF stored # 0
       THEN Serve(s, stored)
       ELSE /\ step' = [step EXCEPT ![s] = "claim"]
            /\ UNCHANGED <<st, held, windowDue, renewSet>>
  /\ UNCHANGED <<hs, worldVars>>

\* Obtain, step 2: claim the lease with the conditional upsert. A lease
\* another slot holds fails the request with errCertificateIssuing, which
\* retries in 5 s.
ObtainClaims(s) ==
  /\ st[s] = "obtaining"
  /\ step[s] = "claim"
  /\ IF NoLease
       THEN step' = [step EXCEPT ![s] = "check"] /\ UNCHANGED <<st, lease>>
       ELSE IF lease = "free"
              THEN step' = [step EXCEPT ![s] = "check"] /\ lease' = s /\ UNCHANGED st
              ELSE Go(s, "retrying", "none") /\ UNCHANGED lease
  /\ UNCHANGED <<held, windowDue, renewSet, hs, stored, dueUpTo, cycles,
                 newOrders, named, faults, lost>>

\* Obtain, step 3: tsnet's CertPair returns the stored certificate if there
\* is one, and otherwise starts an order.
ObtainTsnetChecks(s) ==
  /\ st[s] = "obtaining"
  /\ step[s] = "check"
  /\ IF stored # 0 /\ ~TsnetOrdersBlind
       THEN Serve(s, stored) /\ Release(s)
       ELSE /\ step' = [step EXCEPT ![s] = "issue"]
            /\ UNCHANGED <<st, held, windowDue, renewSet, lease>>
  /\ UNCHANGED <<hs, stored, dueUpTo, cycles, newOrders, named, faults, lost>>

\* Obtain, step 4: the order, naming nothing. Then the lease is released.
ObtainIssues(s) ==
  /\ st[s] = "obtaining"
  /\ step[s] = "issue"
  /\ Release(s)
  /\ \/ /\ Issue(0)
        /\ Serve(s, stored + 1)
        /\ UNCHANGED faults
     \/ /\ Fails
        /\ Go(s, "retrying", "none")
        /\ UNCHANGED <<held, windowDue, renewSet, stored, newOrders, named>>
  /\ UNCHANGED <<hs, dueUpTo, cycles, lost>>

RetryDue(s) ==
  /\ st[s] = "retrying"
  /\ Go(s, "obtaining", "read")
  /\ UNCHANGED <<held, windowDue, renewSet, hs, worldVars>>

\* Serving has three deadlines. A newly held certificate's window is asked
\* for at once and the store re-read only an hour later, so the refresh
\* waits for the window check. Whether a refresh or a renewal fires first
\* when both are due is left open: the rules hold in either order.
RefreshDue(s) ==
  /\ st[s] = "serving"
  /\ ~windowDue[s]
  /\ Go(s, "checkingStore", "none")
  /\ UNCHANGED <<held, windowDue, renewSet, hs, worldVars>>

WindowCheckDue(s) ==
  /\ st[s] = "serving"
  /\ windowDue[s]
  /\ Go(s, "fetchingWindow", "none")
  /\ UNCHANGED <<held, windowDue, renewSet, hs, worldVars>>

\* The renewal deadline needs the window to have been read, and passes only
\* once the held version is due. With PerSlotRenewal the slot's tsnet goes
\* straight to Let's Encrypt.
RenewDue(s) ==
  /\ st[s] = "serving"
  /\ renewSet[s]
  /\ held[s] <= dueUpTo
  /\ Go(s, "renewing", IF PerSlotRenewal THEN "issue" ELSE "claim")
  /\ UNCHANGED <<held, windowDue, renewSet, hs, worldVars>>

\* Adopt a stored version that replaces the held one; otherwise keep
\* serving it. An unreadable store is the same as nothing newer.
StoreChecked(s) ==
  /\ st[s] = "checkingStore"
  /\ IF stored > held[s]
       THEN Serve(s, stored)
       ELSE Go(s, "serving", "none") /\ UNCHANGED <<held, windowDue, renewSet>>
  /\ UNCHANGED <<hs, worldVars>>

\* The ARI window, or two thirds of the lifetime when it cannot be read,
\* gives the renewal time. Later window checks are folded into CyclePasses,
\* which is when Let's Encrypt says the certificate is due.
WindowFetched(s) ==
  /\ st[s] = "fetchingWindow"
  /\ Go(s, "serving", "none")
  /\ windowDue' = [windowDue EXCEPT ![s] = FALSE]
  /\ renewSet' = [renewSet EXCEPT ![s] = TRUE]
  /\ UNCHANGED <<held, hs, worldVars>>

\* Renew, step 1: claim the lease. A lease another slot holds is a failed
\* renewal; the slot keeps serving what it holds and tries again later.
RenewClaims(s) ==
  /\ st[s] = "renewing"
  /\ step[s] = "claim"
  /\ LET next == IF RenewWithoutReread THEN "issue" ELSE "reread" IN
       IF NoLease
         THEN step' = [step EXCEPT ![s] = next] /\ UNCHANGED <<st, lease>>
         ELSE IF lease = "free"
                THEN step' = [step EXCEPT ![s] = next] /\ lease' = s /\ UNCHANGED st
                ELSE Go(s, "renewBackoff", "none") /\ UNCHANGED lease
  /\ UNCHANGED <<held, windowDue, renewSet, hs, stored, dueUpTo, cycles,
                 newOrders, named, faults, lost>>

\* Renew, step 2: re-read the store under the lease, and return what
\* another slot already renewed the held version into.
RenewRereads(s) ==
  /\ st[s] = "renewing"
  /\ step[s] = "reread"
  /\ IF stored > held[s]
       THEN Serve(s, stored) /\ Release(s)
       ELSE /\ step' = [step EXCEPT ![s] = "issue"]
            /\ UNCHANGED <<st, held, windowDue, renewSet, lease>>
  /\ UNCHANGED <<hs, stored, dueUpTo, cycles, newOrders, named, faults, lost>>

\* Renew, step 3: tsnet renews at once, naming the held version as the one
\* replaced. A failure, or tsnet returning the held certificate unchanged,
\* goes to renewBackoff.
RenewIssues(s) ==
  /\ st[s] = "renewing"
  /\ step[s] = "issue"
  /\ Release(s)
  /\ \/ /\ Issue(held[s])
        /\ Serve(s, stored + 1)
        /\ UNCHANGED faults
     \/ /\ Fails
        /\ Go(s, "renewBackoff", "none")
        /\ UNCHANGED <<held, windowDue, renewSet, stored, newOrders, named>>
  /\ UNCHANGED <<hs, dueUpTo, cycles, lost>>

\* The retry time passes; the slot never stopped serving the held version.
BackoffDue(s) ==
  /\ st[s] = "renewBackoff"
  /\ Go(s, "serving", "none")
  /\ UNCHANGED <<held, windowDue, renewSet, hs, worldVars>>

\* Closing the node stops any service; its deferred release frees the lease.
\* The model assumes a stopped request's tsnet order stops with it;
\* HolderOutlivesLease is what happens when it does not.
Close(s) ==
  /\ st[s] # "stopped"
  /\ Go(s, "stopped", "none")
  /\ Release(s)
  /\ hs' = [hs EXCEPT ![s] = "idle"]
  /\ UNCHANGED <<held, windowDue, renewSet, stored, dueUpTo, cycles,
                 newOrders, named, faults, lost>>

----------------------------------------------------------------------------
(* The outside world.                                                      *)

\* A slot holding the lease is lost (its pod dies) before it releases it.
\* Anything it had not yet stored never reached Let's Encrypt; the slot
\* starts again holding nothing, and the lease stays taken until it lapses.
HolderLost(s) ==
  /\ lease = s
  /\ lost < MaxLostHolders
  /\ lost' = lost + 1
  /\ lease' = "orphan"
  /\ Go(s, "obtaining", "read")
  /\ held' = [held EXCEPT ![s] = 0]
  /\ windowDue' = [windowDue EXCEPT ![s] = FALSE]
  /\ renewSet' = [renewSet EXCEPT ![s] = FALSE]
  /\ hs' = [hs EXCEPT ![s] = "idle"]
  /\ UNCHANGED <<stored, dueUpTo, cycles, newOrders, named, faults>>

\* A lease left behind lapses after 3 minutes.
OrphanLapses ==
  /\ lease = "orphan"
  /\ lease' = "free"
  /\ UNCHANGED <<slotVars, stored, dueUpTo, cycles, newOrders, named, faults, lost>>

\* Only with HolderOutlivesLease: the lease of a slot still working under it
\* lapses. The slot carries on and its release later deletes nothing.
LiveLeaseLapses ==
  /\ HolderOutlivesLease
  /\ lease \in Slots
  /\ lost < MaxLostHolders
  /\ lost' = lost + 1
  /\ lease' = "free"
  /\ UNCHANGED <<slotVars, stored, dueUpTo, cycles, newOrders, named, faults>>

\* Only with ObtainOnHandshake: a client reaches a slot holding no
\* certificate, and tsnet, terminating TLS, starts an order unless the store
\* has a certificate.
Handshake(s) ==
  /\ ObtainOnHandshake
  /\ st[s] # "stopped"
  /\ held[s] = 0
  /\ hs[s] = "idle"
  /\ (stored = 0 \/ TsnetOrdersBlind)
  /\ hs' = [hs EXCEPT ![s] = "issuing"]
  /\ UNCHANGED <<st, step, held, windowDue, renewSet, worldVars>>

HandshakeOrderEnds(s) ==
  /\ hs[s] = "issuing"
  /\ hs' = [hs EXCEPT ![s] = "idle"]
  /\ \/ Issue(0) /\ UNCHANGED faults
     \/ Fails /\ UNCHANGED <<stored, newOrders, named>>
  /\ UNCHANGED <<st, step, held, windowDue, renewSet, lease, dueUpTo, cycles, lost>>

\* Time passes until every certificate stored so far is due for renewal.
CyclePasses ==
  /\ cycles < MaxCycles
  /\ dueUpTo < stored
  /\ cycles' = cycles + 1
  /\ dueUpTo' = stored
  /\ UNCHANGED <<slotVars, lease, stored, newOrders, named, faults, lost>>

AllStopped == \A s \in Slots : st[s] = "stopped"

\* Once every slot is closed nothing more happens. This step keeps TLC from
\* reporting that as a deadlock.
Finished ==
  /\ AllStopped
  /\ UNCHANGED vars

Next ==
  \/ \E s \in Slots :
       \/ Approved(s)
       \/ ObtainReadsStore(s)
       \/ ObtainClaims(s)
       \/ ObtainTsnetChecks(s)
       \/ ObtainIssues(s)
       \/ RetryDue(s)
       \/ RefreshDue(s)
       \/ WindowCheckDue(s)
       \/ RenewDue(s)
       \/ StoreChecked(s)
       \/ WindowFetched(s)
       \/ RenewClaims(s)
       \/ RenewRereads(s)
       \/ RenewIssues(s)
       \/ BackoffDue(s)
       \/ Close(s)
       \/ HolderLost(s)
       \/ Handshake(s)
       \/ HandshakeOrderEnds(s)
  \/ OrphanLapses
  \/ LiveLeaseLapses
  \/ CyclePasses
  \/ Finished

\* What a slot does by itself in each state: its poll, the next step of
\* the service the state runs, or the retry when it is due. At most one is
\* enabled per slot at a time, and only the slot's own steps (or a close or
\* a lost holder) disable it, so one weak fairness over all of them is the
\* same as one per action, and much cheaper for TLC.
SlotWorks(s) ==
  \/ Approved(s)
  \/ ObtainReadsStore(s)
  \/ ObtainClaims(s)
  \/ ObtainTsnetChecks(s)
  \/ ObtainIssues(s)
  \/ RetryDue(s)
  \/ StoreChecked(s)
  \/ WindowFetched(s)
  \/ RenewClaims(s)
  \/ RenewRereads(s)
  \/ RenewIssues(s)
  \/ BackoffDue(s)

\* The system acts by itself: polls, service steps, timers, a tsnet order
\* already started, a lapsing lease. Nothing forces a close, a lost holder,
\* a client handshake, a live lease lapsing, or time to pass. The renewal
\* deadline is strongly fair because the hourly refresh keeps taking the
\* slot out of serving for a moment; the refresh does not starve it.
Fairness ==
  /\ \A s \in Slots :
       /\ WF_vars(SlotWorks(s))
       /\ WF_vars(RefreshDue(s))
       /\ WF_vars(WindowCheckDue(s))
       /\ SF_vars(RenewDue(s))
       /\ WF_vars(HandshakeOrderEnds(s))
  /\ WF_vars(OrphanLapses)

Spec == Init /\ [][Next]_vars /\ Fairness

----------------------------------------------------------------------------
(* The rules.                                                              *)

\* At most one new (non-renewal) order per connection, ever.
OneNewOrder == newOrders <= 1

\* Each certificate is replaced at most once.
OneRenewalPerCycle == \A v \in Versions : named[v] <= 1

\* A slot only serves a certificate that was issued and stored.
ServedIsIssued == \A s \in Slots : held[s] <= stored

\* Every slot that is not closed comes to hold the latest stored
\* certificate, however often it changes. Each formula below is one
\* "always eventually", which TLC checks far faster than a leads-to per
\* slot and version; stored only grows, so it says the same: whenever
\* version v is stored, a later moment comes where every live slot holds v
\* or a later one.
Converges ==
  []<>(\A s \in Slots : st[s] = "stopped" \/ held[s] = stored)

\* Nothing stored, or the latest stored certificate due, is always followed
\* by a newer certificate stored, unless every slot is closed. This covers
\* the first issuance (nothing stored counts as due) and every renewal.
Renews == []<>(dueUpTo < stored \/ AllStopped)
=============================================================================
