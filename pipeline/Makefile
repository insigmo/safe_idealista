# Requires Go 1.26+. No cgo: H3 comes from the pure-Go x/h3go package.
export CGO_ENABLED = 0

DATA := ../data

.PHONY: data bcn madrid spain refresh test vet

# Download (cached) sources and regenerate grid.json + review artefacts, the
# Madrid grid in data/madrid/ and the Spain-wide shards in data/spain/.
data:
	go run . -cache .cache -out $(DATA)

# One layer only: Barcelona's grid, Madrid's grid, or the Spain-wide shards.
bcn:
	go run . -cache .cache -out $(DATA) -only bcn

madrid:
	go run . -cache .cache -out $(DATA) -only madrid

spain:
	go run . -cache .cache -out $(DATA) -only spain

# Same, but re-download every source file.
refresh:
	go run . -cache .cache -out $(DATA) -refresh

test:
	go test ./...

vet:
	go vet ./...
