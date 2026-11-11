# syntax=docker/dockerfile:1
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/api .

FROM alpine:3.22
RUN adduser -D -u 10001 app
COPY --from=build /out/api /usr/local/bin/api
USER app
ENV ADDR=:8081
EXPOSE 8081
# DATABASE_URL (postgres://...) must be provided at runtime.
HEALTHCHECK --interval=10s --timeout=3s --retries=5 CMD wget -qO- http://127.0.0.1:8081/api/health >/dev/null || exit 1
ENTRYPOINT ["api"]
