# TapeOut indexer — single static Go binary, no dependencies, no DB.
# Build: docker build -t tapeout-indexer .
# Run:   docker run -p 8080:8080 -v tapeout-data:/data tapeout-indexer
#        (the binary writes its snapshot to /data/tapeout-indexer.json;
#        without a volume the snapshot is lost on container replace and
#        the first sync rebuilds it from chain in ~90 s)
#
# NOTE: the image build was NOT tested on this VM (no Docker here).
# Verify with `docker build -t tapeout-indexer .` on a machine with
# Docker before deploying.
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY *.go ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /tapeout-indexer . \
 && mkdir -p /data && chmod 777 /data

FROM scratch
# CA bundle: the binary calls https://rpc.xlayer.tech over HTTPS, so TLS
# verification needs the system roots (scratch ships none).
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /tapeout-indexer /tapeout-indexer
COPY --from=build /data /data
# Snapshot lives here when --data /data/tapeout-indexer.json is used.
# (No VOLUME instruction: Railway rejects it; a Railway Volume mounted
#  at /data provides persistence instead.)
EXPOSE 8080
# Non-root (65532 is the usual distroless/scratch nobody UID).
# /data is world-writable in the image so the snapshot can be saved
# when a fresh volume is mounted over it.
USER 65532:65532
ENTRYPOINT ["/tapeout-indexer"]
# Default: serve :8080, sync every 5m, snapshot to /data.
# PaaS hosts set $PORT and the binary honours it (see main.go).
CMD ["--data", "/data/tapeout-indexer.json"]
