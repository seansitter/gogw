default: assets
	go build

assets: res/bindata.go
	
res/bindata.go:
	go-bindata -pkg res -o res/bindata.go assets/...
	
install:
	go install

assets-clean:
	if [ -e res/bindata.go ]; then rm res/bindata.go; fi
	
clean: assets-clean
	go clean
	
run: assets default
	go run main.go

all: default install

.PHONY: res/bindata.go