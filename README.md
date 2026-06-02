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

### Examples

```bash
curl -X PUT --data 'hello' 'localhost:8080/cache/greeting?ttl=30'
curl localhost:8080/cache/greeting          # -> hello
curl -i localhost:8080/cache/missing        # -> 404
curl localhost:8080/stats                    # -> {"entries":1}
```
