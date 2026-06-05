# 多阶段构建 Dockerfile

# Stage 1: Build the frontend (Vue3 + Vite)
FROM node:18-alpine AS frontend-builder
WORKDIR /app/views
COPY views/package.json views/package-lock.json* ./
RUN npm install
COPY views/ ./
RUN npm run build

# Stage 2: Build the backend (Go)
FROM golang:1.21-alpine AS backend-builder
WORKDIR /app/server
COPY server/go.mod server/go.sum* ./
RUN go mod download
COPY server/ ./
# Copy the built frontend static files to be embedded or served by Go
COPY --from=frontend-builder /app/views/dist ./views/dist

# Build the statically linked Go binary
RUN CGO_ENABLED=0 GOOS=linux go build -o balanceserver .
RUN CGO_ENABLED=0 GOOS=linux go build -o reset-admin-password ./scripts

# Stage 3: Production minimal image
FROM alpine:latest
WORKDIR /app

RUN apk --no-cache add ca-certificates tzdata

COPY --from=backend-builder /app/server/balanceserver ./balanceserver
COPY --from=backend-builder /app/server/reset-admin-password ./reset-admin-password
COPY --from=backend-builder /app/server/mongodb_config.yaml ./mongodb_config.yaml

EXPOSE 3000
CMD ["./balanceserver"]
