
release: build
	tar cvzf dbmi-1.0.0-macos.tar.gz build/macos-amd64/dbmi LICENSE

.PHONY: build
build:
	mkdir -p build/linux-amd64 build/macos-amd64 build/windows-amd64
	go build -o build/macos-amd64/dbmi dbmi.go
	GOOS=linux GOARCH=amd64 go build -o build/linux-amd64/dbmi dbmi.go
	GOOS=windows GOARCH=amd64 go build -o build/windows-amd64/dbmi.exe dbmi.go
