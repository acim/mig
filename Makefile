.PHONY: lint test test-cov

lint:
	@golangci-lint run

test:
	@docker-compose up --build --abort-on-container-exit --exit-code-from mig

update:
	@go get -u all
