.PHONY: lint test update

lint:
	@golangci-lint run --fix

start:
	@docker-compose up --build --renew-anon-volumes

stop:
	@docker-compose down

test:
	@go test -race -coverprofile=coverage.out ./...
	@report=$$(go tool cover -func coverage.out); \
	echo "$$report"; \
	coverage=$$(printf "%s\n" "$$report" | awk '/^total:/ {gsub(/%/, "", $$3); print $$3}'); \
	threshold=$${COVERAGE_THRESHOLD:-80}; \
	awk "BEGIN { exit !($$coverage >= $$threshold) }" || \
		(echo "coverage $$coverage% is below $$threshold%" && exit 1)

update:
	@go get -u all
	@go mod tidy
