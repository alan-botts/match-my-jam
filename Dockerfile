# syntax=docker/dockerfile:1.7
FROM golang:1.25-alpine AS build
WORKDIR /src

RUN apk add --no-cache ca-certificates git

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/match-my-jam ./cmd/match-my-jam

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/match-my-jam /app/match-my-jam
ENV MMJ_DB_PATH=/data/mmj.db
ENV PORT=8080
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/app/match-my-jam"]
