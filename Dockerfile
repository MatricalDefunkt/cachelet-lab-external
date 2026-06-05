# syntax=docker/dockerfile:1

# ---- build stage ----
# Pin to the same major Go version as the go.mod directive (go 1.23).
FROM golang:1.23 AS build
WORKDIR /src

# Cache module downloads as their own layer: they only re-run when go.mod/go.sum
# change, not on every source edit.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Static, stripped binary so it runs in a scratch/distroless image with no libc.
# CGO is off for a fully static binary; -trimpath keeps build paths out of it.
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
    -o /out/cachelet ./cmd/cachelet

# ---- runtime stage ----
# distroless static: no shell, no package manager, runs as an unprivileged user.
# It still ships CA certs and tzdata, which cachelet does not need today but are
# cheap and avoid surprises if it grows outbound calls.
FROM gcr.io/distroless/static:nonroot
COPY --from=build /out/cachelet /cachelet

EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/cachelet"]
