.PHONY: setup build test acceptance run

setup:
	git config core.hooksPath .githooks
	@echo "Git-хуки включены: .githooks/pre-commit"

build:
	go build ./...

test:
	go test -race -count=1 ./...

acceptance:
	cd acceptance && MARKETPLACE_SRC=$(CURDIR) go test -race -count=1 -v ./...

run: build
	JWT_SECRET=test-secret go run . --addr=:8080
