FROM golang:1.27.4-bookworm AS build
ENV CGO_ENABLED=0 GOTOOLCHAIN=local
WORKDIR /src
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ ./
RUN go build -trimpath -ldflags="-s -w" -o /out/paste-anywhere ./cmd/paste-anywhere \
    && mkdir -p /out/data && touch /out/data/.keep

FROM scratch
COPY --from=build /out/paste-anywhere /paste-anywhere
COPY --from=build --chown=65532:65532 /out/data/ /data/
USER 65532:65532
WORKDIR /data
ENV PASTE_ANYWHERE_DB=/data/clipboard.db
EXPOSE 8080
ENTRYPOINT ["/paste-anywhere"]
CMD ["serve", "--listen", "0.0.0.0:8080"]
