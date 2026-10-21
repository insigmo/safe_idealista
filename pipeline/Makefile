# Requires Go 1.26+. No cgo: H3 comes from the pure-Go x/h3go package.
export CGO_ENABLED = 0

DATA := ../data

.PHONY: data refresh test vet

# Download (cached) sources and regenerate grid.json + review artefacts.
data:
	go run . -cache .cache -out $(DATA)

# Same, but re-download every source file.
refresh:
	go run . -cache .cache -out $(DATA) -refresh

test:
	go test ./...

vet:
	go vet ./...
