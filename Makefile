.PHONY: lint test update

lint:
	@golangci-lint run \
		--enable-all \
		--disable copyloopvar \
		--disable deadcode \
		--disable depguard \
		--disable execinquery \
		--disable exhaustivestruct \
		--disable golint \
		--disable gomnd \
		--disable ifshort \
		--disable interfacer \
		--disable intrange \
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
