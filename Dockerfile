FROM node:22-alpine AS web-builder
WORKDIR /src
COPY apps/web/package*.json apps/web/
RUN npm --prefix apps/web ci
COPY apps/web apps/web
RUN npm --prefix apps/web run build

FROM golang:1.25.5-alpine AS api-builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY apps/api apps/api
COPY internal internal
RUN go build -trimpath -ldflags="-s -w" -o /out/sing-panel-api ./apps/api

FROM alpine:3.22
RUN adduser -D -H -u 10001 singpanel && mkdir -p /data /app/web && chown -R singpanel:singpanel /data /app
USER singpanel
WORKDIR /app
COPY --from=api-builder /out/sing-panel-api /app/sing-panel-api
COPY --from=web-builder /src/apps/web/dist /app/web
ENV SING_PANEL_ADDR=:8080
ENV SING_PANEL_DB=/data/sing-panel.db
ENV SING_PANEL_WEB_DIR=/app/web
EXPOSE 8080
CMD ["/app/sing-panel-api"]
