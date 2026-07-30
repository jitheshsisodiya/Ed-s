.PHONY: help dev-up dev-down test test-backend test-relay test-client test-admin test-mobile test-sdk \
        lint build migrate-up migrate-down proto

help:
	@echo "make dev-up          - start local stack via docker compose"
	@echo "make dev-down        - stop local stack"
	@echo "make test            - run all test suites"
	@echo "make lint            - run all linters"
	@echo "make build           - build all components"
	@echo "make migrate-up      - apply DB migrations against DATABASE_URL"
	@echo "make migrate-down    - roll back the last DB migration"
	@echo "make proto           - regenerate gRPC stubs from proto/"

dev-up:
	docker compose -f deploy/docker/docker-compose.yml up -d --build

dev-down:
	docker compose -f deploy/docker/docker-compose.yml down

test: test-backend test-relay test-client test-sdk

test-backend:
	cd backend && go test ./... -race

test-relay:
	cd relay && go test ./... -race

test-client:
	cd client && go test ./... -race

test-admin:
	cd admin && npm test --if-present

test-mobile:
	cd mobile && flutter test

test-sdk:
	cd sdk/go && go test ./...
	cd sdk/js && npm test

lint:
	cd backend && go vet ./...
	cd relay && go vet ./...
	cd client && go vet ./...
	cd admin && npm run lint
	npx --yes @redocly/cli lint api/openapi.yaml

build:
	cd backend && go build ./...
	cd relay && go build ./...
	cd client && go build ./...
	cd admin && npm run build

migrate-up:
	migrate -path db/migrations -database "$$DATABASE_URL" up

migrate-down:
	migrate -path db/migrations -database "$$DATABASE_URL" down 1

proto:
	cd proto && buf generate
