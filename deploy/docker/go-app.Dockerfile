ARG GO_VERSION=1.26.5
FROM golang:${GO_VERSION}-alpine AS build

WORKDIR /src
COPY go.mod go.sum go.work ./
RUN go mod download

COPY . .
ARG APP=api
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/cytisus ./apps/${APP}

FROM alpine:3.24
RUN addgroup -S cytisus && adduser -S -G cytisus cytisus
WORKDIR /app
COPY --from=build /out/cytisus /app/cytisus
COPY db/migrations /app/db/migrations
USER cytisus
ENTRYPOINT ["/app/cytisus"]
