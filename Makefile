.PHONY: generate css dev build test

# Compile all templ templates to Go code.
generate:
	templ generate ./web/templates/...

# Generate Tailwind CSS output from source (requires tailwindcss CLI in PATH or ./tailwindcss.exe).
# Download: https://github.com/tailwindlabs/tailwindcss/releases/latest (tailwindcss-windows-x64.exe)
css:
	tailwindcss -i web/static/css/input.css -o web/static/css/app.css --minify

# Development: generate templates + CSS then run server.
dev: generate
	go run ./cmd/server

# Full build: generate + CSS + compile binary.
build: generate
	go build -o app-server.exe ./cmd/server

# Run integration tests (requires TEST_DATABASE_URL in .env).
test:
	go test ./internal/core -v
