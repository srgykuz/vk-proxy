NAME=vk-proxy
VERSION=0.10
BIN_DIR=bin
DIST_DIR=dist

build:
	@mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/$(NAME) .

bin:
	@mkdir -p $(BIN_DIR)
	GOOS=linux GOARCH=amd64 go build -o $(BIN_DIR)/$(NAME)-linux-amd64 .
	GOOS=linux GOARCH=arm64 go build -o $(BIN_DIR)/$(NAME)-linux-arm64 .
	GOOS=darwin GOARCH=amd64 go build -o $(BIN_DIR)/$(NAME)-darwin-amd64 .
	GOOS=darwin GOARCH=arm64 go build -o $(BIN_DIR)/$(NAME)-darwin-arm64 .
	GOOS=windows GOARCH=amd64 go build -o $(BIN_DIR)/$(NAME)-windows-amd64.exe .
	GOOS=windows GOARCH=arm64 go build -o $(BIN_DIR)/$(NAME)-windows-arm64.exe .
	GOOS=android GOARCH=arm64 go build -o $(BIN_DIR)/$(NAME)-android-arm64 .

dist: bin
	@mkdir -p $(DIST_DIR)/tmp

	cp $(BIN_DIR)/$(NAME)-linux-amd64 $(DIST_DIR)/tmp/$(NAME)
	cp README.md stub.jpg stub.mp4 systemd.txt logrotate.txt $(DIST_DIR)/tmp/
	cp config.template.json $(DIST_DIR)/tmp/config.json
	tar -czf $(DIST_DIR)/$(NAME)-$(VERSION)-linux-amd64.tar.gz -C $(DIST_DIR)/tmp $(NAME) README.md stub.jpg stub.mp4 config.json systemd.txt logrotate.txt

	cp $(BIN_DIR)/$(NAME)-linux-arm64 $(DIST_DIR)/tmp/$(NAME)
	cp README.md stub.jpg stub.mp4 systemd.txt logrotate.txt $(DIST_DIR)/tmp/
	cp config.template.json $(DIST_DIR)/tmp/config.json
	tar -czf $(DIST_DIR)/$(NAME)-$(VERSION)-linux-arm64.tar.gz -C $(DIST_DIR)/tmp $(NAME) README.md stub.jpg stub.mp4 config.json systemd.txt logrotate.txt

	cp $(BIN_DIR)/$(NAME)-darwin-amd64 $(DIST_DIR)/tmp/$(NAME)
	cp README.md stub.jpg stub.mp4 $(DIST_DIR)/tmp/
	cp config.template.json $(DIST_DIR)/tmp/config.json
	tar -czf $(DIST_DIR)/$(NAME)-$(VERSION)-macos-amd64.tar.gz -C $(DIST_DIR)/tmp $(NAME) README.md stub.jpg stub.mp4 config.json

	cp $(BIN_DIR)/$(NAME)-darwin-arm64 $(DIST_DIR)/tmp/$(NAME)
	cp README.md stub.jpg stub.mp4 $(DIST_DIR)/tmp/
	cp config.template.json $(DIST_DIR)/tmp/config.json
	tar -czf $(DIST_DIR)/$(NAME)-$(VERSION)-macos-arm64.tar.gz -C $(DIST_DIR)/tmp $(NAME) README.md stub.jpg stub.mp4 config.json

	cp $(BIN_DIR)/$(NAME)-windows-amd64.exe $(DIST_DIR)/tmp/$(NAME).exe
	cp README.md stub.jpg stub.mp4 $(DIST_DIR)/tmp/
	cp config.template.json $(DIST_DIR)/tmp/config.json
	cp win-secret.bat $(DIST_DIR)/tmp/secret.bat
	cp win-version.bat $(DIST_DIR)/tmp/version.bat
	cd $(DIST_DIR)/tmp && zip ../$(NAME)-$(VERSION)-windows-amd64.zip $(NAME).exe README.md stub.jpg stub.mp4 config.json secret.bat version.bat

	cp $(BIN_DIR)/$(NAME)-windows-arm64.exe $(DIST_DIR)/tmp/$(NAME).exe
	cp README.md stub.jpg stub.mp4 $(DIST_DIR)/tmp/
	cp config.template.json $(DIST_DIR)/tmp/config.json
	cp win-secret.bat $(DIST_DIR)/tmp/secret.bat
	cp win-version.bat $(DIST_DIR)/tmp/version.bat
	cd $(DIST_DIR)/tmp && zip ../$(NAME)-$(VERSION)-windows-arm64.zip $(NAME).exe README.md stub.jpg stub.mp4 config.json secret.bat version.bat

	cp $(BIN_DIR)/$(NAME)-android-arm64 $(DIST_DIR)/tmp/$(NAME)
	cp README.md stub.jpg stub.mp4 $(DIST_DIR)/tmp/
	cp config.template.json $(DIST_DIR)/tmp/config.json
	tar -czf $(DIST_DIR)/$(NAME)-$(VERSION)-android-arm64.tar.gz -C $(DIST_DIR)/tmp $(NAME) README.md stub.jpg stub.mp4 config.json

	rm -rf $(DIST_DIR)/tmp

	cd $(DIST_DIR) && shasum -a 256 *.tar.gz *.zip > $(NAME)-$(VERSION)-checksums.txt

clean:
	rm -rf $(BIN_DIR) $(DIST_DIR)
