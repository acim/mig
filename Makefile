.PHONY: lint start stop test update

COMPOSE ?= podman-compose

lint:
	@golangci-lint run

start:
	@$(COMPOSE) up --build --renew-anon-volumes

stop:
	@$(COMPOSE) down

test:
	@go test -race -coverprofile=coverage.out -count=1 ./...
	@report=$$(go tool cover -func coverage.out); \
	echo "$$report"; \
	coverage=$$(printf "%s\n" "$$report" | awk '/^total:/ {gsub(/%/, "", $$3); print $$3}'); \
	badge=$$(awk -F'coverage-|%25' '/img.shields.io\/badge\/coverage-/ {print $$2; exit}' README.md); \
	threshold=$${COVERAGE_THRESHOLD:-90}; \
	if [ -z "$$coverage" ]; then echo "coverage total not found" && exit 1; fi; \
	if [ -z "$$badge" ]; then echo "README coverage badge not found" && exit 1; fi; \
	if [ "$$coverage" != "$$badge" ]; then \
		echo "README coverage badge $$badge% does not match measured coverage $$coverage%" && exit 1; \
	fi; \
	awk "BEGIN { exit !($$coverage >= $$threshold) }" || \
		(echo "coverage $$coverage% is below $$threshold%" && exit 1)

update:
	@go get -u all
	@go mod tidy
