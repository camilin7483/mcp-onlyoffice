BINARY := mcp-onlyoffice
CONFIG_DIR := ${HOME}/.config/mcp-onlyoffice
STORAGE_DIR := ${HOME}/mcp-onlyoffice/documents

.PHONY: all build clean install uninstall run

all: build

build:
	go build -o $(BINARY) ./cmd/mcp-onlyoffice

install: build
	mkdir -p $(CONFIG_DIR) $(STORAGE_DIR)
	install -m 755 $(BINARY) ${HOME}/.local/bin/$(BINARY)
	@echo "Installed to ${HOME}/.local/bin/$(BINARY)"
	@echo "Config: $(CONFIG_DIR)/config.json"
	@echo "Storage: $(STORAGE_DIR)"
	@test -f $(CONFIG_DIR)/config.json || cp config.example.json $(CONFIG_DIR)/config.json

uninstall:
	rm -f ${HOME}/.local/bin/$(BINARY)
	@echo "Removed ${HOME}/.local/bin/$(BINARY)"

clean:
	rm -f $(BINARY)

run: build
	./$(BINARY)

dev:
	go run ./cmd/mcp-onlyoffice
