.PHONY: all build test bench tidy run-mock run-gateway run-bench clean

all: build

build:
	cd web && npm run build
	mkdir -p internal/server/dist
	cp -r web/dist/* internal/server/dist/
	go build -o bin/kvring ./cmd/kvring

test:
	go test -v ./...

bench:
	go test -bench=BenchmarkRouter -benchmem ./internal/ring

run-mock:
	go run ./cmd/kvring mock-cluster --ports 8001,8002,8003

run-gateway:
	go run ./cmd/kvring serve --config ./examples/kvring.yaml

run-bench:
	go run ./cmd/kvring bench -n 500 -c 20

clean:
	rm -rf bin/ internal/server/dist/ web/dist/
