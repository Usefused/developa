FROM node:22-alpine AS ui
WORKDIR /src
COPY package.json package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY react-router.config.ts vite.config.ts ./
COPY internal/webui internal/webui
COPY api api
RUN npm run api:validate
RUN npm run build

FROM golang:1.26.0-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=ui /src/internal/webui/dist internal/webui/dist
RUN go run ./cmd/openapi -check
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

FROM alpine:3.23
LABEL org.opencontainers.image.title="Developa" \
      org.opencontainers.image.description="Self-hosted Go code intelligence API and visual explorer" \
      org.opencontainers.image.source="https://github.com/Usefused/developa" \
      org.opencontainers.image.documentation="https://github.com/Usefused/developa/blob/main/README.md"
RUN apk add --no-cache ca-certificates git && addgroup -S developa && adduser -S -G developa developa
COPY --from=build /out/server /usr/local/bin/server
USER developa:developa
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/server"]
