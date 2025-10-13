FROM bitnami/kubectl:latest as kubectl

# Use the official Golang image as the base image
FROM golang:1.23.3

# Install kubectl
RUN apt-get update && \
  apt-get install -y apt-transport-https gnupg && \
  apt-get update

# Set the working directory inside the container
WORKDIR /app

COPY --from=kubectl /opt/bitnami/kubectl/bin/kubectl /usr/local/bin/

# Copy the Go modules manifests
COPY go.mod go.sum ./

# Download Go modules
RUN go mod download

# Copy the source code
COPY . .

# Build all binaries from the cmd folder
RUN for d in ./cmd/*/ ; do \
  go build -o /app/bin/$(basename $d) $d ; \
  done
