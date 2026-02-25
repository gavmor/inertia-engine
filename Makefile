.PHONY: build run test clean

build:
	go build -o inertia-engine main.go

run: build
	./inertia-engine --context ../logs/inertia-context-$(shell date +%Y-%m-%d).json

dry-run: build
	./inertia-engine --context ../logs/inertia-context-$(shell date +%Y-%m-%d).json --dry-run

test:
	go test ./...

clean:
	rm -f inertia-engine

install: build
	cp inertia-engine ~/.local/bin/

help:
	@echo "Available targets:"
	@echo "  build    - Compile the binary"
	@echo "  run      - Build and run with today's context"
	@echo "  dry-run  - Build and run without executing td commands"
	@echo "  test     - Run tests"
	@echo "  clean    - Remove binary"
	@echo "  install  - Install to ~/.local/bin"
