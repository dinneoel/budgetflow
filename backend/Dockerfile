FROM golang:1.26.1-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/server ./cmd/server \
    && CGO_ENABLED=0 go build -trimpath -tags pgx5 -o /out/migrate github.com/golang-migrate/migrate/v4/cmd/migrate

FROM alpine:3.23
RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -u 10001 app
WORKDIR /app
COPY --from=build /out/ /usr/local/bin/
COPY migrations/*.sql ./migrations/
USER app
EXPOSE 8080
CMD ["server"]
