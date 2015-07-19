.PHONY: default cluegetter deps fmt all assets clean
export GOPATH:=$(shell pwd)

BUILDTAGS=debug
default: all

deps: assets
	go get -tags '$(BUILDTAGS)' -d -v cluegetter/...

cluegetter: deps
	go install -tags '$(BUILDTAGS)' cluegetter

bin/go-bindata:
	GOOS="" GOARCH="" go get github.com/jteeuwen/go-bindata/go-bindata

assets: bin/go-bindata
	bin/go-bindata -nomemcopy -pkg=assets -tags=$(BUILDTAGS) \
                -debug=$(if $(findstring debug,$(BUILDTAGS)),true,false) \
                -o=src/cluegetter/assets/assets_$(BUILDTAGS).go \
                assets/...

fmt:
	go fmt cluegetter/...

all: fmt cluegetter

clean:
	go clean -i -r cluegetter
	rm -rf src/cluegetter/assets/
