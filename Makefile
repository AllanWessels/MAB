.PHONY: proto build test up down logs tidy clean

PROTOC ?= protoc
GEN_DIR := gen

proto:
	$(PROTOC) -I proto \
		--go_out=. --go_opt=module=mab \
		--go-grpc_out=. --go-grpc_opt=module=mab \
		proto/bandit.proto

tidy:
	go mod tidy

build: proto tidy
	go build ./...

test: proto tidy
	go test ./...

up:
	docker compose up --build -d

down:
	docker compose down -v

logs:
	docker compose logs -f mab

clean:
	rm -rf $(GEN_DIR)
