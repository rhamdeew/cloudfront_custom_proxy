build: 
	GOOS=linux GOARCH=amd64 go build -o cloudfront_custom_proxy main.go && mv cloudfront_custom_proxy release/
