# syntax=docker/dockerfile:1.3
FROM golang:1.20.1

WORKDIR /app
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod tidy

COPY . .

CMD [".docker/test.sh"]
