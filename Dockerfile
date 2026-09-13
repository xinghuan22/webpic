FROM golang:1.26-alpine AS build
WORKDIR /src
RUN apk add --no-cache ca-certificates
RUN mkdir -p /data/manga && chown 65532:65532 /data/manga && chmod 0700 /data/manga
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -tags=nomsgpack -trimpath -ldflags="-s -w" -o /image-gateway ./cmd/server

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /image-gateway /image-gateway
COPY --from=build --chown=65532:65532 /data/manga /data/manga
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/image-gateway"]
