# Use an official Golang runtime as a parent image
FROM golang:1.22.5 as builder

# Set the working directory inside the container
WORKDIR /app


# Copy the `nostr_threads` source files into the container
COPY . ./nostr_threads

# Build the `nostr_threads` binary
WORKDIR /app/nostr_threads
RUN go mod tidy && go build -o /app/bin/nostr_threads .

# Use a smaller base image for the final stage
FROM debian:latest

# Install necessary certificates
RUN apt-get update && apt-get install -y ca-certificates && rm -rf /var/lib/apt/lists/*

# Copy the built binaries from the builder stage
COPY --from=builder /app/bin/nostr_threads /usr/local/bin/nostr_threads
# COPY ./conf.json /app/conf.json

# Expose any necessary ports (if needed)
EXPOSE 8081
EXPOSE 6060

# Command to run both `nak` and `nostr_threads`
CMD ["sh", "-c", "nostr_threads"]