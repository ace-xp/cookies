# Delivery migrations

`20260811120000_delivery_decision_workflows.up.sql` adds the immutable Phase C
Decision -> Selection -> CompiledWorkflow authority spine. It reuses the
existing immutable platform-configuration store and adds no executable
credentials or remote runtime. Database CHECK constraints require
`ready_for_final_approval` and reject `remote_write_enabled=true`.

`20260811121000_delivery_contract_hash_algorithm_width.up.sql` and
`20260811122000_delivery_contract_hash_identity_compat.up.sql`, together with
`20260811123000_delivery_alert_provenance_width_compat.up.sql`, are forward-only
compatibility corrections for environments that applied an early draft of the
v2 configuration migration. They restore the frozen 64-character algorithm
column, the intended `(id, version)` immutable identity, and the complete
simulator provenance width without rewriting any payload or canonical hash.

Owner: Delivery team.

`20260810120000_delivery_platform_configuration_runtime.up.sql` 是 DeliveryIntent/PlatformConfiguration 的增量切换迁移。它创建不可变 Intent 与判别式配置存储，为 Plan/ChangeSet 增加 schema 判别器，并为 Approval 增加显式 Intent/配置绑定。迁移不对旧 `config_json`、`target_snapshot`、canonical hash 或 approval action hash 执行 `UPDATE`。

`20260731120000_delivery_approval_content_hashes.up.sql` intentionally adds
`delivery_plan_versions.canonical_hash` as nullable for the SQL phase. The
`cookies-migrate` command immediately runs
`delivery.BackfillPlanCanonicalHashes`, which recalculates every existing
version with the same RFC 8785 JCS + SHA-256 Go canonicalizer used by new
writes, fills only missing hashes, rejects any non-empty mismatched hash
instead of blessing changed immutable content, verifies that no hashes are
missing, and then makes the column `NOT NULL`.

The command then runs `delivery.BackfillLegacyApprovals`. Any ChangeSet created
before version-bound approvals with the compatibility
`approved_by`/`approved_at` projection is
converted once into an immutable `delivery_approvals` authority record,
including the original approval time, the fixed 24-hour expiry, the inferred
approval-time ChangeSetVersion, content/action hashes, `execute_mock` scope,
budget snapshot, and mock provenance.

Apply migrations through `go run ./cmd/cookies-migrate`; do not fill plan
hashes with MySQL JSON/hash functions or a second canonicalization
implementation.

`20260803100000_delivery_execution_scenarios.up.sql` is additive for legacy approval
data: historic succeeded executions receive mock/success defaults and remain
readable. New execution writes require a canonical Go request hash and an
idempotency key; the unique scope prevents duplicate executions while
`delivery_execution_steps` persists fixture progress and recovery evidence.
After the SQL phase, `cookies-migrate` runs
`delivery.BackfillLegacyExecutions`: each historic execution is linked to
its immutable approval, receives a Project-scoped `legacy-<execution_id>` key
and a canonical compatibility request hash (`expected_version=0`), and gets
one synthetic succeeded verification Step plus redacted mock evidence. The
backfill is transactional and idempotent; a second migration run changes no
rows.

`20260803103000_delivery_execution_scenarios_compatibility.up.sql` makes an
already-applied early execution schema safe for durable queued work: `completed_at`
is nullable until terminal state and the idempotency uniqueness scope is
exactly Organization + Project + key (not ChangeSet). It is intentionally a
forward-only correction rather than an edit to a migration an environment may
already have recorded.
# Monitoring compatibility

The monitoring migration is additive: it preserves legacy metric rows with
`fixture_version=legacy` and `window_sequence=1`, backfills `data_through` from
the existing window end, and extends uniqueness for multi-window fixtures. It
adds `delivery_alerts` with organization/project fingerprint uniqueness and CAS
versions. No legacy metric or alert data is deleted or rewritten destructively.

# Mock/Replay observatory

`20260812120000_delivery_observatory_runs.up.sql` adds immutable observatory runs and append-only operator feedback. Runs reference an existing DecisionSelection and freeze the exact Decision, Configuration, and Workflow hashes. CHECK constraints admit only `mock`/`replay`, `observe_existing`/`prepare_new_local_form`, and `remote_write_enabled = FALSE`; the canonical input hash is unique per project for deterministic replay. Feedback references the immutable run, supports `accepted`/`modified`/`rejected`, and uses a project-scoped idempotency key. The migration does not rewrite any earlier payload or hash.

`20260812121000_delivery_observatory_feedback_outcome.up.sql` freezes the reviewed run outcome directly on each feedback row. It is a forward-only compatibility migration because the initial observatory migration may already have been applied; no existing immutable JSON or canonical hash is rewritten.
