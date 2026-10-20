# Builds one binary from this module. TARGET picks which:
#   docker build --build-arg TARGET=./cmd/loadgen .
FROM golang:1.25 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGET=.
RUN CGO_ENABLED=0 go build -o /out/app ${TARGET}

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/app /app
ENTRYPOINT ["/app"]
