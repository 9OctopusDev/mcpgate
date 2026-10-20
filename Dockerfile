FROM golang:1.27.3-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/mcpgate ./cmd/mcpgate

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/mcpgate /mcpgate
USER nonroot:nonroot
ENTRYPOINT ["/mcpgate"]
CMD ["serve", "--config", "/etc/mcpgate/gateway.yaml"]
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 CMD ["/mcpgate", "healthcheck", "--url", "http://127.0.0.1:9090/readyz"]
