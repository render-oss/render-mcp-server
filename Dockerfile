FROM golang:1.24.1-alpine AS build
ARG VERSION="dev"

# Set the working directory
WORKDIR /build

# Install git
RUN --mount=type=cache,target=/var/cache/apk \
    apk add git

# Build the MCP server
# go build automatically download required module dependencies to /go/pkg/mod
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=bind,target=. \
    CGO_ENABLED=0 go build -ldflags="-s -w -X cfg.version=${VERSION} " \
    -o /bin/render-mcp-server main.go

# Make a stage to run the app
FROM gcr.io/distroless/base-debian12
# Set the working directory
WORKDIR /server
# Copy the binary from the build stage
COPY --from=build /bin/render-mcp-server .
# Set default config path (inside container)
ENV RENDER_CONFIG_PATH=/config/mcp-server.yaml
# Use ENTRYPOINT instead of CMD so that additional user-provided args are passed to the server
ENTRYPOINT ["./render-mcp-server"]
CMD []
