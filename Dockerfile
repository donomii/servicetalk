# Dockerfile — multi-stage build of the demo cluster image.
#
# Builds the smt runtime from source and bundles the repo's demo classes
# (Main.smt, Counter.smt, Dog.st, CCounter/) into an alpine image.  Used
# by docker-compose.yml for the local cluster smoke test.
#
# For shipping a real app:
#   1. Run ./bundle.sh path/to/your/app dist/yourapp
#   2. Build the resulting Dockerfile inside the bundle:
#        docker build -t yourapp dist/yourapp
#
# That route does not require a Go toolchain at the destination and
# produces a smaller image.

FROM golang:1.21-alpine AS builder
WORKDIR /src
COPY go.mod ./
COPY *.go ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/smt .

FROM alpine:3.19
# gcc + musl-dev so foreign C classes compile during the build below
# (and on first spawn if not pre-built).
RUN apk add --no-cache gcc musl-dev
WORKDIR /app
COPY --from=builder /out/smt /app/smt
COPY *.smt *.st ./
COPY CCounter/ /app/CCounter/

# Pre-build every foreign-class binary for the image's arch.  Removes
# the first-spawn $CC delay and means the running cluster doesn't need
# the C toolchain available — but keeping gcc in the image is harmless
# and makes ad-hoc class additions painless.
RUN /app/smt prebuild

ENTRYPOINT ["/app/smt"]
CMD ["coordinator", "--main", ""]
