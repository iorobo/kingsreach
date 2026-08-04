# Kingsreach — single-container image for Easypanel (or any Docker host).
# Stage 1 bundles the Babylon.js frontend, stage 2 builds the Go server, and
# the runtime image serves both from one binary on :8080.

FROM node:22-alpine AS client
WORKDIR /client
COPY client/package.json client/package-lock.json ./
RUN npm ci
COPY client/ ./
RUN npm run build          # writes ../web (bundle + index.html + .gz)

FROM golang:1.25-alpine AS server
WORKDIR /src
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ ./
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/kingsreach ./cmd/server

FROM alpine:3.20
RUN adduser -D app
WORKDIR /app
COPY --from=server /out/kingsreach /app/kingsreach
COPY --from=client /web /app/web
ENV PORT=8080 STATIC_DIR=/app/web
EXPOSE 8080
USER app
CMD ["/app/kingsreach"]
