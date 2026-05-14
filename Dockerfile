# syntax=docker/dockerfile:1.7

# ---- protoc stage: generate Go bindings from proto/ ----
FROM golang:1.22-bookworm AS proto
RUN apt-get update && apt-get install -y --no-install-recommends \
    protobuf-compiler unzip ca-certificates && rm -rf /var/lib/apt/lists/*
ENV GOBIN=/go/bin
RUN go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.34.2 \
 && go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.5.1
WORKDIR /src
COPY proto/ proto/
RUN protoc -I proto \
      --go_out=. --go_opt=module=mab \
      --go-grpc_out=. --go-grpc_opt=module=mab \
      proto/bandit.proto \
 && test -f gen/mabpb/bandit.pb.go \
 && test -f gen/mabpb/bandit_grpc.pb.go

# ---- build stage ----
FROM golang:1.22-bookworm AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download || true
COPY . .
COPY --from=proto /src/gen ./gen
RUN go mod tidy \
 && go test ./... \
 && CGO_ENABLED=0 GOOS=linux go build -o /out/mab-server ./cmd/server

# ---- runtime stage ----
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/mab-server /mab-server
EXPOSE 50051
USER nonroot:nonroot
ENTRYPOINT ["/mab-server"]
