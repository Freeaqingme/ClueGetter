export GOPATH:=$(shell pwd)

GO        ?= go
PKG       := ./src/cluegetter/
BUILDTAGS := debug

.PHONY: default
default: all

.PHONY: deps
deps: assets
	go get -tags '$(BUILDTAGS)' -d -v cluegetter/...
	go get github.com/robfig/glock
	git diff /dev/null GLOCKFILE | ./bin/glock apply .

.PHONY: cluegetter
cluegetter: deps binary

.PHONY: binary
binary: LDFLAGS += -X "main.buildTag=$(shell git describe --dirty --tags)"
binary: LDFLAGS += -X "main.buildTime=$(shell date -u '+%Y-%m-%d %H:%M:%S UTC')"
binary:
	go install -tags '$(BUILDTAGS)' -ldflags '$(LDFLAGS)' cluegetter

.PHONY: release
release: BUILDTAGS=release
release: cluegetter

.PHONY: bin/go-bindata
bin/go-bindata:
	GOOS="" GOARCH="" go get github.com/jteeuwen/go-bindata/go-bindata

assets: bin/go-bindata
	bin/go-bindata -nomemcopy -pkg=assets -prefix="assets/" -tags=$(BUILDTAGS) \
                -debug=$(if $(findstring debug,$(BUILDTAGS)),true,false) \
                -o=src/cluegetter/assets/assets_$(BUILDTAGS).go \
                assets/...

.PHONY: fmt
fmt:
	go fmt cluegetter/...

.PHONY: all
all: fmt cluegetter

.PHONY: clean
clean:
	rm -rf bin/
	rm -rf pkg/
	rm -rf src/cluegetter/assets/
	go clean -i -r cluegetter

.PHONY: deb
deb: release

.PHONY: check
check:
	@echo "checking for forbidden imports"
	@echo "vet"
	@! $(GO) tool vet $(PKG) 2>&1 | \
	  grep -vE '^vet: cannot process directory .git'
	@echo "vet --shadow"
	@! $(GO) tool vet --shadow $(PKG) 2>&1
	@echo "golint"
	@! golint $(PKG)
	@echo "varcheck"
	@! varcheck -e $(PKG) | \
	  grep -vE '(_string.go|sql/parser/(yacctab|sql\.y))'
	@echo "gofmt (simplify)"
	@! gofmt -s -d -l . 2>&1 | grep -vE '^\.git/'
	@echo "goimports"
	@! goimports -l . | grep -vF 'No Exceptions'
