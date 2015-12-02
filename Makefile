.PHONY: default cluegetter release deps fmt all assets clean
export GOPATH:=$(shell pwd)

BUILDTAGS=debug
default: all

deps: assets
	go get -tags '$(BUILDTAGS)' -d -v cluegetter/...
	go get github.com/robfig/glock
	git diff /dev/null GLOCKFILE | ./bin/glock apply .

cluegetter: deps
	go install -tags '$(BUILDTAGS)' cluegetter

release: BUILDTAGS=release
release: cluegetter

bin/go-bindata:
	GOOS="" GOARCH="" go get github.com/jteeuwen/go-bindata/go-bindata

assets: bin/go-bindata
	bin/go-bindata -nomemcopy -pkg=assets -prefix="assets/" -tags=$(BUILDTAGS) \
                -debug=$(if $(findstring debug,$(BUILDTAGS)),true,false) \
                -o=src/cluegetter/assets/assets_$(BUILDTAGS).go \
                assets/...

fmt:
	go fmt cluegetter/...

all: fmt cluegetter

clean:
	rm -rf bin/
	rm -rf pkg/
	rm -rf src/cluegetter/assets/
	go clean -i -r cluegetter
