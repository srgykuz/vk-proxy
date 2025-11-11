NAME=vk-proxy
VERSION=0.6
BIN_DIR=bin
DIST_DIR=dist

build:
	@mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/$(NAME) .

build-all:
	@mkdir -p $(BIN_DIR)
	GOOS=linux GOARCH=amd64 go build -o $(BIN_DIR)/$(NAME)_linux_amd64 .
	GOOS=linux GOARCH=arm64 go build -o $(BIN_DIR)/$(NAME)_linux_arm64 .
	GOOS=darwin GOARCH=amd64 go build -o $(BIN_DIR)/$(NAME)_darwin_amd64 .
	GOOS=darwin GOARCH=arm64 go build -o $(BIN_DIR)/$(NAME)_darwin_arm64 .
	GOOS=windows GOARCH=amd64 go build -o $(BIN_DIR)/$(NAME)_windows_amd64.exe .
	GOOS=windows GOARCH=arm64 go build -o $(BIN_DIR)/$(NAME)_windows_arm64.exe .

package-all: build-all
	@mkdir -p $(DIST_DIR)/tmp

	cp $(BIN_DIR)/$(NAME)_linux_amd64 $(DIST_DIR)/tmp/$(NAME)
	cp README.md $(DIST_DIR)/tmp/
	cp config.template.json $(DIST_DIR)/tmp/config.json
	tar -czf $(DIST_DIR)/$(NAME)_$(VERSION)_linux_amd64.tar.gz -C $(DIST_DIR)/tmp $(NAME) README.md config.json

	cp $(BIN_DIR)/$(NAME)_linux_arm64 $(DIST_DIR)/tmp/$(NAME)
	cp README.md $(DIST_DIR)/tmp/
	cp config.template.json $(DIST_DIR)/tmp/config.json
	tar -czf $(DIST_DIR)/$(NAME)_$(VERSION)_linux_arm64.tar.gz -C $(DIST_DIR)/tmp $(NAME) README.md config.json

	cp $(BIN_DIR)/$(NAME)_darwin_amd64 $(DIST_DIR)/tmp/$(NAME)
	cp README.md $(DIST_DIR)/tmp/
	cp config.template.json $(DIST_DIR)/tmp/config.json
	tar -czf $(DIST_DIR)/$(NAME)_$(VERSION)_darwin_amd64.tar.gz -C $(DIST_DIR)/tmp $(NAME) README.md config.json

	cp $(BIN_DIR)/$(NAME)_darwin_arm64 $(DIST_DIR)/tmp/$(NAME)
	cp README.md $(DIST_DIR)/tmp/
	cp config.template.json $(DIST_DIR)/tmp/config.json
	tar -czf $(DIST_DIR)/$(NAME)_$(VERSION)_darwin_arm64.tar.gz -C $(DIST_DIR)/tmp $(NAME) README.md config.json

	cp $(BIN_DIR)/$(NAME)_windows_amd64.exe $(DIST_DIR)/tmp/$(NAME).exe
	cp README.md $(DIST_DIR)/tmp/
	cp config.template.json $(DIST_DIR)/tmp/config.json
	cd $(DIST_DIR)/tmp && zip ../$(NAME)_$(VERSION)_windows_amd64.zip $(NAME).exe README.md config.json

	cp $(BIN_DIR)/$(NAME)_windows_arm64.exe $(DIST_DIR)/tmp/$(NAME).exe
	cp README.md $(DIST_DIR)/tmp/
	cp config.template.json $(DIST_DIR)/tmp/config.json
	cd $(DIST_DIR)/tmp && zip ../$(NAME)_$(VERSION)_windows_arm64.zip $(NAME).exe README.md config.json

	rm -rf $(DIST_DIR)/tmp

checksums: package-all
	cd $(DIST_DIR) && shasum -a 256 *.tar.gz *.zip > $(NAME)_$(VERSION)_checksums.txt

clean:
	rm -rf $(BIN_DIR) $(DIST_DIR)
