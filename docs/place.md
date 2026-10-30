# A place in a distributed graph

**Status: place semantics used by the first distributed execution path.** The authority keeps places in memory; [remote transition executors and arc transports](remote-execution.md) transfer proposed values while the authority alone commits. Durable recovery and replicated authority are not implemented.

## Definition

A **place** is a named, typed location for one graph instance's token state. Its marking is either empty or contains one immutable token of the place's declared type. The graph gives the place a computational identity and type. The graph instance supplies its current marking. Two graph instances have separate markings even when they use the same graph definition.

A place is a graph vertex, not a process, socket, queue, or executor. Arcs connect it to transitions:

```text
transition A -- output arc --> place P -- input arc --> transition B
```

A place can have zero, one, or multiple incoming and outgoing transition arcs. A source place can receive an initial or permitted external token without a producer transition. A sink place can be inspected or drained by a permitted external adapter without a consumer transition. External ingress and egress need explicit authorization and semantics; an ordinary internal place is not automatically exposed.

## Authority and state

For the first distributed design, **one logical authority per graph instance** owns the authoritative marking of every place and makes firing decisions. Transition executors can run elsewhere. Logical authority does not require a separate service per place, and the initial design does not assume replicated or distributed authority.

The authority tracks each place's token and any reservation or output lock belonging to an attempt. It must be able to recover the complete committed token value for authorized consumers. Where token bytes reside, whether copies are cached or passed directly, and how tokens travel along arcs are open transport decisions; possession of a copy does not grant authority to consume or produce a token. A reservation or lock is execution metadata, not a token and not an additional graph vertex.

| Place condition | Meaning |
| --- | --- |
| Empty, available | No committed token; an eligible producer can reserve it as an output. |
| Full, available | One committed token; an eligible consumer can reserve it as an input. |
| Full, input reserved | The committed token remains in the marking, but one active attempt has exclusive authority to consume it on commit. |
| Empty, output locked | An active attempt has exclusive authority to produce into the place on commit; there is no committed output token yet. |
| Full, input reserved and output locked | The same attempt consumes and replaces the token through a self-loop on successful commit. |

A place with a reserved input can still be inspected as holding its committed token. An output lock does not make a future token visible. An attempt can start only if *all* its inputs and outputs can be acquired together, including the self-loop case. The authority must prevent competing attempts or external actions from consuming a reserved input or filling a locked output. For an empty output, a concurrent competing producer cannot acquire it. An occupied place cannot accept another independent output token.

## Firing across places

A transition firing involves all of its input and output places, not an independent transaction per arc:

1. The authority checks and atomically reserves all inputs and locks all outputs for one uniquely identified attempt. If any required place is unavailable, no reservation or lock is left behind.
2. The transition executor receives the attempt's input tokens and runs. Tokens received over input arcs are copies or representations for that attempt; receiving them does not remove the committed tokens from the places.
3. The executor returns its proposed output tokens. The authority validates the attempt, every output port, token and type before changing the marking. Transmitting a proposed token over an output arc does not by itself place a committed token in a place.
4. If the attempt succeeds and retains authority to commit, the authority atomically consumes all input tokens and publishes all output tokens. For a self-loop, the old token is atomically replaced.
5. If the authority finalizes the attempt as failed, canceled, expired or invalid before commit, it releases its reservations and locks, and the prior committed marking remains unchanged. Results arriving after finalization cannot commit. External actions by an executor cannot be undone by restoring the marking.

Multiple transitions consuming one place compete for its single token. Only one successful firing can consume it. Multiple producers compete for its available slot. This is not broadcast: use an explicit duplication transition and separate places when several consumers each need a token. The order in which contenders are chosen and fairness guarantees are not specified.

For `A -> P -> B`, A's successful commit makes one token present in P. B can then reserve that token as part of acquiring all of its own inputs and outputs. If B fails before committing, P still holds the same token. If B succeeds, P becomes empty, unless B also has an output arc to P, in which case its commit replaces the token.

## Failure and transport boundary

The single authority is the source of truth for the graph instance; transport moves messages or token representations, not ownership of committed marking. Remote executors and port adapters need attempt identity, authorization, compatible token representation, and a way to distinguish proposed values from committed state. Specific handshake, delivery, serialization, retry, duplicate-message and endpoint rules remain to be designed.

While the authority is running and can determine an attempt's status, it can finalize failure and reject late results. If the authority itself becomes unreachable or crashes, participants must not infer that an attempt committed or rolled back merely because a connection timed out. Persistence, recovery, uncertain outcomes, restart identity and partition behavior are not yet specified; the first version may have to stop progress until a safe outcome is known. This design does not promise durable or highly available execution.

## Acceptance criteria for a future implementation

- At most one committed token of the declared type occupies each place in a graph instance.
- Competing consumers cannot both commit consumption of the same token; competing producers cannot both fill the same available place.
- Failed acquisition changes no place's marking, reservations, or locks. A failed or invalid attempt preserves its complete prior marking and releases its own reservations/locks.
- A successful multi-place firing changes all its inputs and outputs atomically; a self-loop replaces its token atomically.
- Late or duplicate messages do not cause an extra commit; transport receipt alone does not consume or publish a token.
- Source/sink access, executor disconnection, authority loss and recovery are handled according to an explicitly specified contract. Manual clock, timeout and supervised-attempt semantics remain preserved.
