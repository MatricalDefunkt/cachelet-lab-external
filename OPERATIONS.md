# Operating cachelet

This covers running cachelet in production: what its observability surface is,
what healthy looks like, what to alert on, and the design choices behind it.

## Endpoints for operators

| Path       | Purpose   | Meaning                                                                 |
|------------|-----------|-------------------------------------------------------------------------|
| `/metrics` | Prometheus | RED metrics + cache stats + Go/process runtime metrics.                |
| `/healthz` | Liveness  | Always `200` while the process can serve. Failing it should *restart* the pod. |
| `/readyz`  | Readiness | `200` when accepting traffic, `503` during shutdown drain.             |

### Metrics exposed

- `cachelet_http_requests_total{method,route,status}` — request rate and errors.
  Labelled by the **route pattern** (`/cache/{key}`), never the raw key, so
  cardinality stays bounded.
- `cachelet_http_request_duration_seconds{method,route}` — latency histogram.
- `cachelet_cache_hits_total` / `cachelet_cache_misses_total` — the hit ratio,
  the headline signal for a cache.
- `cachelet_cache_entries` — entries currently held (sampled at scrape time).
  This is the proxy for memory growth, which is cachelet's main failure mode.
- Standard `go_*` and `process_*` collectors (goroutines, GC, heap, FDs, CPU).

## What healthy looks like

All pods `Ready`. `cachelet_http_requests_total` is dominated by `2xx`/`204`
(and `404`s, which are normal cache misses at the HTTP layer — see below). The
hit ratio is steady for the workload, p99 of
`cachelet_http_request_duration_seconds` is in the low milliseconds (everything
is in-memory under a mutex), and `cachelet_cache_entries` is flat or sawtooths
with the janitor rather than climbing without bound. `go_goroutines` is small
and stable.

## What I would alert on

1. **Memory growth toward the limit.** cachelet is a TTL-only cache with no max
   size — entries written without a TTL live until deleted, so the store can grow
   without bound and the pod will eventually OOMKill (the manifest sets
   `requests == limits` for memory precisely so it dies cleanly instead of
   taking the node with it). Alert on `container_memory_working_set_bytes`
   approaching the limit, and watch `cachelet_cache_entries` for an unbounded
   climb as the leading indicator.

2. **5xx rate.** `rate(cachelet_http_requests_total{status=~"5.."}[5m]) > 0`.
   Cachelet has no dependencies, so a 5xx is an internal bug or a wedged pod, not
   an upstream problem. Note that `404`s are *not* errors here — they are cache
   misses and are expected; alert on `5xx`, not `4xx`.

3. **Pods not ready.** `kube_deployment_status_replicas_unavailable{deployment="cachelet"} > 0`
   for a few minutes. With a single replica (see below) this is a full outage.

A secondary one worth having: a **hit ratio collapse**
(`rate(hits)/rate(hits+misses)` dropping sharply). It does not page, but it
usually means a client changed key patterns or TTLs and is about to drive load
onto whatever cachelet sits in front of.

## What I would check first

- **Memory alert:** scrape `/metrics` and look at `cachelet_cache_entries`. If it
  is climbing steadily, clients are writing entries without a TTL (or with very
  long ones). Confirm via the workload, not cachelet — it does not track per-key
  age. Short term, rolling the pod clears the store; the real fix is TTLs on the
  client side or a bounded-size policy in cachelet (a known non-goal today).

- **5xx alert:** check pod logs (`request failed` lines are logged at error level
  with method/path) and `go_goroutines` / restart count. A liveness failure plus
  restarts points at a wedged or panicking process; logs will show the cause.

- **Not-ready alert:** `kubectl get pods` and `kubectl describe`. Distinguish a
  crash-loop (liveness failing, restarts climbing) from a normal rollout drain
  (readiness `503` while the process is shutting down — expected and brief).

## Design choices and tradeoffs

- **Single replica by default.** The cache is in-memory and per-pod with no
  replication or sharding. Two replicas behind one Service means two independent
  caches: a key written through pod A is a miss on pod B. That is acceptable for
  a best-effort cache (a miss just refetches), but it makes the hit ratio worse
  and is surprising, so the default is one replica. Scaling horizontally is fine
  if you accept those semantics; it is the documented knob, not a silent default.

- **Liveness vs readiness are genuinely different.** Liveness has no dependencies
  to check, so `/healthz` is a bare `200` — the only thing that fails it is a
  process that cannot answer, which is exactly when a restart helps. `/readyz`
  carries the one piece of real state: a flag flipped to not-ready at the start of
  shutdown. On `SIGTERM`, the process fails readiness, waits a short drain window
  (`drainDelay`, 5s) for kube-proxy to pull it from endpoints, then closes the
  listener with a 5s grace period for in-flight requests. This avoids dropping
  requests onto a pod that is about to die.

- **Probes and `/metrics` bypass the request metrics.** They are high-frequency
  and would dominate `cachelet_http_requests_total`, and `/metrics` must not
  recurse through its own instrumentation. Only the cache API and `/stats` are
  counted.

- **Status captured via a response wrapper.** The metrics middleware wraps the
  `ResponseWriter` and sits outside error translation, so the recorded status is
  the one actually sent to the client (including `404`/`500` from the error
  adapter), not an approximation.

- **`client_golang` rather than hand-rolled exposition.** It brings the Go and
  process collectors for free and a correct exposition format. Each `Server` owns
  a private registry instead of the global default, so constructing multiple
  servers (the tests do) never panics on duplicate registration.

- **Single port.** Metrics, probes, and the API share `:8080`. A hardened setup
  would split `/metrics` and the probes onto a separate admin port so they are
  not exposed alongside the public API; that was left out as out of scope here.

- **Container.** Multi-stage build to a `distroless/static:nonroot` image: static
  `CGO_ENABLED=0` binary, no shell or package manager, runs unprivileged with a
  read-only root filesystem and all capabilities dropped. The one consequence
  worth noting: distroless has no shell, so there is no `preStop` `exec` hook —
  draining is handled in-process (the readiness flip above) rather than with a
  `preStop sleep`.

## If I had more time

- A bounded/LRU eviction policy (or a configurable max entry count) so memory is
  capped by design rather than by the OOM limit.
- A separate admin port for `/metrics` and probes.
- An example PrometheusRule with the alerts above encoded, and a small dashboard.
