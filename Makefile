.PHONY: lint test update

lint:
	@golangci-lint run \
		--enable-all \
		--disable deadcode \
		--disable depguard \
		--disable exhaustivestruct \
		--disable golint \
		--disable ifshort \
		--disable interfacer \
		--disable maligned \
		--disable nosnakecase \
		--disable scopelint \
		--disable structcheck \
		--disable varcheck \
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
