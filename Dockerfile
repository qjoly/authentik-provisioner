# Build a static binary and ship it in a distroless image.
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/provisioner ./cmd/provisioner

FROM gcr.io/distroless/static:nonroot
COPY --from=build /out/provisioner /provisioner
# Default config path; mount your config at /config/config.yaml (see deploy/).
ENTRYPOINT ["/provisioner", "-config", "/config/config.yaml"]
