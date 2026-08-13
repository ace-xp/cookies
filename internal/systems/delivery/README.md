# Delivery system

Owner: Delivery team. It owns plans, change sets, platform entities, and
evidence; shared Computer Use only provides a controlled execution capability.

The Phase C authority spine is:

`DeliveryIntent + PlatformConfiguration + immutable facts -> DeliveryDecision -> DecisionSelection -> CompiledDeliveryWorkflow -> ready_for_final_approval`

Decision generation and workflow compilation are deterministic and side-effect free. Persisting a selection creates a new immutable platform-configuration version, but never mutates the Plan, creates a formal approval, or enables a platform write. Every compiled `remote_write` step is blocked with `PHASE_C_REMOTE_WRITE_PROHIBITED`; the database also rejects workflows whose `remote_write_enabled` is true.

The Phase C observatory extends that spine with deterministic `mock` and `replay` runs. A run binds the exact Decision, Configuration, and Workflow hashes, executes only `observe` or `prepare_local_form`, freezes selector/page/evidence references and field diffs, and records the prohibited remote-write boundary as evidence. Data-quality blocks, platform drift, and runner failures are separate outcomes. Operator feedback is append-only (`accepted`, `modified`, or `rejected`) and never mutates the original decision, run, or evidence. No observatory code invokes a connector, Computer Use, a platform adapter, or network I/O.

The Recommendation lifecycle is not an active optimization model. New generate/accept/reject operations are restricted to owner-scoped historical Tour runs; non-Tour projects use DeliveryDecision exclusively.
