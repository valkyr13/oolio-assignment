# Build stage: compile a static binary.
# GO_VERSION "1" is the latest Go 1.x, so the build never fails because
# go.mod asks for a newer Go than the image has. Pin an exact version
# (for example 1.24) for fully reproducible builds.
ARG GO_VERSION=1
FROM golang:${GO_VERSION}-alpine AS build
WORKDIR /src

# Dependencies first, so Docker reuses this layer when only code changes.
COPY go.mod go.sum* ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

# Run stage: a minimal image with no shell or package manager, running as a
# non-root user. Only the binary and its data files are copied in.
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/server /app/server
COPY data/products.json data/valid_coupons.txt /app/data/

ENV ADDR=:8080
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/app/server"]
