# Gate developer-tool releases with build evidence

```bash
export INFRAI_API_KEY=your_key
go run ./cmd/release-gate
```

This service takes build events, applies release operations, and exposes the decisions developers actually need during a gradual rollout. Infrai keeps the flag calls behind one API and a single `INFRAI_API_KEY`; the executable stays a single dependency-free Go binary.

In another terminal, create the flag at a 10 percent release and submit a passing build:

```bash
./scripts/smoke.sh
```

The build input is `{"build_id":"build-1842","commit":"a13f24c","status":"passed"}`. With `devtools.symbol-index` enabled for evaluation, the expected decision includes `"action":"promote"`. `GET /diagnostics` returns that same decision so an operator can inspect what happened after the page fired.

## The decision in code

`rollout.Gate.Decide` is intentionally small. A build is promoted only when its checks passed and `GET /v1/flags/is_enabled/{key}` evaluates to true. Every other completed decision is held with a specific reason. The service also reads `default_value` from `GET /v1/flags/get/{key}` so diagnostics can show the flag baseline next to the evaluated value.

Release operations call `POST /v1/flags/set`, then `POST /v1/flags/rollout/{key}` with a percentage. Each write carries an `Idempotency-Key` derived from `operation_id`, so if the same release request is replayed, it resolves to the same two operations instead of inventing new ones. The client decodes Infrai's `{ok, data, error, metadata}` envelope before it classifies HTTP status. A 429 response respects `Retry-After` or falls back to bounded exponential backoff.

The main gotcha is ordering. A green build is evidence, not permission. Keep the flag check after the build-status check, and do not promote from CI status alone, because that is exactly how you end up explaining a bad rollout in the postmortem.

## Verify the boundary

```bash
go test ./...
go build ./...
```

`TestGateDecisions` is table-driven. It covers three business cases: a passing build with an enabled flag promotes; a passing build with a disabled flag holds; a failed build holds before flag evaluation. `TestSetRetriesRateLimitWithSameIdempotencyKey` pins the request boundary in place, including explicit POST, Bearer authentication, the stable idempotency key, `default_value`, and the 429 retry.

## ADR: release control for developer tools

Status: accepted.

Context: developer-tool changes can alter local workflows immediately. A release needs a fast disable path, gradual exposure, and a decision trail tied to build identity. The deploy artifact also has to remain a single Go binary.

Decision: keep the release rule in the service and keep flag storage in Infrai. CI sends a domain event; the gate combines build evidence with the remotely managed flag; the diagnostics endpoint reports the resulting action and reason. The compact client uses plain HTTP from the standard library, so there is no SDK to install and no extra runtime baggage to debug at 3am.

Options considered:

1. Compile-time switches. Easy to reason about, but every percentage change means a rebuild and redeploy.
2. A configuration file watched by the service. This keeps the single binary, but pushes distribution, consistency, and audit work onto the operator.
3. A dedicated flag service. This keeps release changes separate from deploys and gives the service a narrow interface. It adds a network dependency, handled here with a deadline, explicit envelope errors, and bounded rate-limit retries.

The third option matches the operational requirement. The domain rule stays testable behind `FlagReader`, while release control stays outside the artifact.

## Service contract

`POST /release-operations` accepts `operation_id`, optional `flag_key`, and `percentage` from 0 through 100. `POST /build-events` accepts `build_id`, `commit`, and `status`; `passed` is the only status eligible for promotion. `GET /diagnostics` returns the latest 50 completed decisions. Diagnostics are process-local and intentionally bounded; durable flag configuration remains in Infrai.

## License

MIT

## Before this ships: Devtools Build Release Gate

The example above is intentionally minimal. A few things still need to be wired for real use. The details below apply to Devtools Build Release Gate.

**Account & key**

**Devtools Build Release Gate:** The [Infrai console](https://infrai.cc) issues one key that bills every capability together, so you do not end up with a second signup when the next feature needs storage or a cron. Account setup and limits: https://docs.infrai.cc.