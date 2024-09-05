# syntax=docker/dockerfile:1.2

# Use a smaller base image for the builder stage
FROM golang:1.22.5-alpine AS builder

# Install git, as it's needed for cloning the nak repository
RUN apk add --no-cache git

# Set environment variables for private repo access
ARG GITHUB_USERNAME
ARG GITHUB_TOKEN
ENV GOPRIVATE=github.com/paulcapestany
RUN git config --global url."https://${GITHUB_USERNAME}:${GITHUB_TOKEN}@github.com/".insteadOf "https://github.com/"

# Set the working directory inside the container
WORKDIR /app

# Copy only go.mod and go.sum to leverage caching of dependencies
COPY go.mod go.sum ./nostr_threads/

# Use a build cache for Go modules
RUN --mount=type=cache,target=/go/pkg/mod cd nostr_threads && go mod download

# Clone the `nak` repository and download dependencies
RUN git clone https://github.com/fiatjaf/nak.git && cd nak && go mod download

# Copy the entire source code for `nostr_threads`
COPY . ./nostr_threads

# Build the `nak` binary
WORKDIR /app/nak
RUN --mount=type=cache,target=/root/.cache/go-build go build -trimpath -ldflags '-s -w' -o /app/bin/nak .

# Build the `nostr_threads` binary
WORKDIR /app/nostr_threads
RUN --mount=type=cache,target=/root/.cache/go-build go build -trimpath -ldflags '-s -w' -o /app/bin/nostr_threads .

# Use a Distroless base image for the final stage
FROM gcr.io/distroless/base

# Set the working directory in the final image
WORKDIR /app

# Copy the built binaries from the builder stage
COPY --from=builder /app/bin/nostr_threads /usr/local/bin/nostr_threads
COPY --from=builder /app/bin/nak /usr/local/bin/nak

# Expose necessary ports
EXPOSE 8081
EXPOSE 6060

# Command to run the `nostr_threads` binary
CMD ["nostr_threads"]