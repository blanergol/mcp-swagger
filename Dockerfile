FROM golang:1.25 AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/mcp-server ./cmd/mcp-server

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app

COPY --from=builder /out/mcp-server /app/mcp-server

ENV TRANSPORT=streamable
ENV HTTP_ADDR=:8080

EXPOSE 8080

ENTRYPOINT ["/app/mcp-server"]
