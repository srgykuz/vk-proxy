NAME=vk-proxy
VERSION=0.7
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
	cp README.md Dockerfile docker-compose.yml $(DIST_DIR)/tmp/
	cp config.template.json $(DIST_DIR)/tmp/config.json
	cp systemd.service $(DIST_DIR)/tmp/$(NAME).service
	tar -czf $(DIST_DIR)/$(NAME)-$(VERSION)-linux-amd64.tar.gz -C $(DIST_DIR)/tmp $(NAME) README.md Dockerfile docker-compose.yml config.json $(NAME).service

	cp $(BIN_DIR)/$(NAME)-linux-arm64 $(DIST_DIR)/tmp/$(NAME)
	cp README.md Dockerfile docker-compose.yml $(DIST_DIR)/tmp/
	cp config.template.json $(DIST_DIR)/tmp/config.json
	cp systemd.service $(DIST_DIR)/tmp/$(NAME).service
	tar -czf $(DIST_DIR)/$(NAME)-$(VERSION)-linux-arm64.tar.gz -C $(DIST_DIR)/tmp $(NAME) README.md Dockerfile docker-compose.yml config.json $(NAME).service

	cp $(BIN_DIR)/$(NAME)-darwin-amd64 $(DIST_DIR)/tmp/$(NAME)
	cp README.md Dockerfile docker-compose.yml $(DIST_DIR)/tmp/
	cp config.template.json $(DIST_DIR)/tmp/config.json
	tar -czf $(DIST_DIR)/$(NAME)-$(VERSION)-darwin-amd64.tar.gz -C $(DIST_DIR)/tmp $(NAME) README.md Dockerfile docker-compose.yml config.json

	cp $(BIN_DIR)/$(NAME)-darwin-arm64 $(DIST_DIR)/tmp/$(NAME)
	cp README.md Dockerfile docker-compose.yml $(DIST_DIR)/tmp/
	cp config.template.json $(DIST_DIR)/tmp/config.json
	tar -czf $(DIST_DIR)/$(NAME)-$(VERSION)-darwin-arm64.tar.gz -C $(DIST_DIR)/tmp $(NAME) README.md Dockerfile docker-compose.yml config.json

	cp $(BIN_DIR)/$(NAME)-windows-amd64.exe $(DIST_DIR)/tmp/$(NAME).exe
	cp README.md Dockerfile docker-compose.yml $(DIST_DIR)/tmp/
	cp config.template.json $(DIST_DIR)/tmp/config.json
	cd $(DIST_DIR)/tmp && zip ../$(NAME)-$(VERSION)-windows-amd64.zip $(NAME).exe README.md Dockerfile docker-compose.yml config.json

	cp $(BIN_DIR)/$(NAME)-windows-arm64.exe $(DIST_DIR)/tmp/$(NAME).exe
	cp README.md Dockerfile docker-compose.yml $(DIST_DIR)/tmp/
	cp config.template.json $(DIST_DIR)/tmp/config.json
	cd $(DIST_DIR)/tmp && zip ../$(NAME)-$(VERSION)-windows-arm64.zip $(NAME).exe README.md Dockerfile docker-compose.yml config.json

	cp $(BIN_DIR)/$(NAME)-android-arm64 $(DIST_DIR)/tmp/$(NAME)
	cp README.md $(DIST_DIR)/tmp/
	cp config.template.json $(DIST_DIR)/tmp/config.json
	tar -czf $(DIST_DIR)/$(NAME)-$(VERSION)-android-arm64.tar.gz -C $(DIST_DIR)/tmp $(NAME) README.md config.json

	rm -rf $(DIST_DIR)/tmp

	cd $(DIST_DIR) && shasum -a 256 *.tar.gz *.zip > $(NAME)-$(VERSION)-checksums.txt

clean:
	rm -rf $(BIN_DIR) $(DIST_DIR)
