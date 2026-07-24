# syntax=docker/dockerfile:1.7
FROM node:22.18-alpine AS web-build
WORKDIR /src/web
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.26.5-alpine AS go-build
RUN apk add --no-cache ca-certificates
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
COPY --from=web-build /src/internal/webui/dist/ internal/webui/dist/
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/family-dashboard ./cmd/server

FROM scratch
COPY --from=go-build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=go-build /out/family-dashboard /family-dashboard
USER 10001:10001
EXPOSE 8080
ENV FAMILY_DASHBOARD_ADDRESS=:8080
ENV FAMILY_DASHBOARD_DATA_DIR=/data
ENTRYPOINT ["/family-dashboard"]
