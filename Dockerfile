# --- build frontend ---
FROM node:20-alpine AS frontend
WORKDIR /app/web/frontend
COPY web/frontend/package.json web/frontend/package-lock.json* web/frontend/yarn.lock* ./
RUN npm install
COPY web/frontend/ ./
RUN npm run build

# --- build binaries ---
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /app/web/dist ./web/dist
RUN CGO_ENABLED=0 go build -o /out/glkvm-vnc ./cmd/glkvm-vnc \
 && CGO_ENABLED=0 go build -o /out/glkvm-vnc-agent ./cmd/glkvm-vnc-agent

# --- runtime ---
FROM alpine:3.20
RUN adduser -D -u 10001 app
COPY --from=build /out/glkvm-vnc /usr/local/bin/glkvm-vnc
COPY --from=build /out/glkvm-vnc-agent /usr/local/bin/glkvm-vnc-agent
USER app
EXPOSE 8600
ENV GLKVM_VNC_DB=/data/glkvm-vnc.db
VOLUME ["/data"]
ENTRYPOINT ["glkvm-vnc"]
