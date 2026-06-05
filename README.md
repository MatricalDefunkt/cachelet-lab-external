# cachelet

A small in-memory key/value cache service with per-entry TTL, exposed over HTTP.

It is intentionally minimal: a single-process cache with lazy expiry on read and
a background janitor that periodically evicts expired entries.

## Scope and limitations

cachelet is deliberately small. Known non-goals in the current version:

- **Unbounded entry count.** Eviction is by TTL only — there is no maximum size
  and no LRU policy. Entries written without a TTL live until explicitly
  deleted, so the store can grow without bound. It is a TTL cache, not a bounded
  LRU.
- **Single process, in-memory.** No persistence and no replication.
- **1 MiB per-entry limit.** `PUT` bodies larger than 1 MiB are rejected.

## Layout

```
.
├── cmd/cachelet/
│   └── main.go        # process entrypoint: HTTP server + graceful shutdown
├── cache/             # the cache.Store (TTL map, janitor)
│   ├── store.go
│   └── store_test.go
└── server/            # HTTP layer over a cache.Store
    ├── handler.go     # apiHandler adapter + middleware chain
    ├── middleware.go  # logRequests (request observability)
    ├── server.go      # routes and handlers
    └── server_test.go
```

## Build, test, run

```bash
make build      # compile ./...
make test       # go test ./...
make race       # go test -race ./...
make vet        # go vet ./...

go run ./cmd/cachelet   # start the server (CACHELET_ADDR, default :8080)
```

## API

| Method   | Path           | Body  | Description                                            |
|----------|----------------|-------|--------------------------------------------------------|
| `GET`    | `/cache/{key}` | —     | Returns the value as `text/plain`, or `404` if absent. |
| `PUT`    | `/cache/{key}` | value | Stores the body. Optional `?ttl=<seconds>` sets expiry. `204` on success. |
| `DELETE` | `/cache/{key}` | —     | Removes the key. Always `204`.                         |
| `GET`    | `/stats`       | —     | `{"entries":<n>}`.                                     |

### Operational endpoints

| Method | Path       | Description                                                         |
|--------|------------|---------------------------------------------------------------------|
| `GET`  | `/metrics` | Prometheus metrics (HTTP RED + cache hits/misses/size + Go runtime). |
| `GET`  | `/healthz` | Liveness. Always `200` while the process can serve.                 |
| `GET`  | `/readyz`  | Readiness. `200` normally, `503` while draining for shutdown.       |

See [OPERATIONS.md](OPERATIONS.md) for what to alert on, what healthy looks
like, and the design tradeoffs behind these.

### Examples

```bash
curl -X PUT --data 'hello' 'localhost:8080/cache/greeting?ttl=30'
curl localhost:8080/cache/greeting          # -> hello
curl -i localhost:8080/cache/missing        # -> 404
curl localhost:8080/stats                    # -> {"entries":1}
curl localhost:8080/metrics                  # -> Prometheus exposition
```

## Container

```bash
docker build -t cachelet .
docker run -p 8080:8080 cachelet
```

Multi-stage build to `distroless/static:nonroot`: a static binary running
unprivileged with a read-only root filesystem.

## Kubernetes

```bash
kubectl apply -f deploy/        # Deployment + Service (probes, resources, scrape annotations)
```

`deploy/optional/servicemonitor.yaml` adds a Prometheus Operator `ServiceMonitor`;
apply it explicitly on a cluster running the operator. It is kept out of `deploy/`
so `kubectl apply -f deploy/` does not require the `monitoring.coreos.com` CRDs.
See [OPERATIONS.md](OPERATIONS.md) for the single-replica and scrape choices.
