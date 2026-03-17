NAME ?= schema-deletion-tool
PLUGIN_NAME ?= confluent-schema_registry-cleanup
GO_LDFLAGS='-w -s'
GOBUILD=CGO_ENABLED=1 go build -trimpath -ldflags $(GO_LDFLAGS)

build: build-local
build-local:
	$(GOBUILD) -o $(NAME)

build-plugin:
	$(GOBUILD) -o $(PLUGIN_NAME)

test:
	go test -v ./...

test-integration:
	go test -v -tags=integration ./...

clean:
	rm -f $(NAME)
	rm -f $(PLUGIN_NAME)

.PHONY: build build-local build-plugin test test-integration clean
