.PHONY: lint test update

lint:
	@golangci-lint run \
		--enable-all \
		--disable copyloopvar \
		--disable depguard \
		--disable intrange \
		--disable tenv \
		--disable varnamelen \
		--fix

start:
	@docker-compose up --build --renew-anon-volumes

stop:
	@docker-compose down

test:
	@go test -race -coverprofile=coverage.out ./...
	@go tool cover -func coverage.out

update:
	@go get -u all
