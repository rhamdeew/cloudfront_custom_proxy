.PHONY: build run clean all test test-coverage

all: build

build:
	go build -o cloudfront_custom_proxy main.go

run: build
	./cloudfront_custom_proxy

clean:
	rm -f cloudfront_custom_proxy

test:
	go test -v ./...