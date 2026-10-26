# murettod: the Muretto sync API. Build from the repository root (the Go module root):
#   docker build -f server/Dockerfile -t murettod .
# Static binary (CGO_ENABLED=0), distroless base with CA certificates, non-root user.

FROM golang:1.27.3-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY internal/contract ./internal/contract
COPY schema ./schema
COPY server ./server
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/murettod ./server/cmd/murettod

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/murettod /murettod
USER nonroot:nonroot
# Railway (and most hosts) set PORT; murettod then listens on :$PORT.
EXPOSE 8080
ENTRYPOINT ["/murettod"]
