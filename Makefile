export GOPATH:=$(shell pwd)

GO        ?= go
PKG       := ./src/cluegetter/
BUILDTAGS := debug
VERSION   ?= $(shell git describe --dirty --tags | sed 's/^v//' )

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
binary: LDFLAGS += -X "main.buildTag=v$(VERSION)"
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

.PHONY: test
test:
	go test -tags '$(BUILDTAGS)' -ldflags '$(LDFLAGS)' cluegetter/...

.PHONY: deb
deb: release
	rm -rf pkg_root/
	mkdir -p pkg_root/lib/systemd/system/
	cp dist/cluegetter.service pkg_root/lib/systemd/system/cluegetter.service
	mkdir -p pkg_root/etc/default
	cp dist/debian/defaults pkg_root/etc/default/cluegetter
	mkdir -p pkg_root/usr/bin/
	cp bin/cluegetter pkg_root/usr/bin/cluegetter
	mkdir -p pkg_root/usr/share/doc/cluegetter
	cp LICENSE pkg_root/usr/share/doc/cluegetter/
	cp CHANGELOG.md pkg_root/usr/share/doc/cluegetter/
	cp mysql.sql pkg_root/usr/share/doc/cluegetter/
	cp DDL-Changes.sql pkg_root/usr/share/doc/cluegetter/
	mkdir -p pkg_root/etc/cluegetter
	cp cluegetter.conf.dist pkg_root/etc/cluegetter/cluegetter.conf
	mkdir -p pkg_root/etc/logrotate.d
	cp dist/debian/logrotate pkg_root/etc/logrotate.d/cluegetter
	fpm \
		-n cluegetter \
		-C pkg_root \
		-s dir \
		-t deb \
		-v $(VERSION) \
		--force \
		--deb-compression bzip2 \
		--after-install dist/debian/postinst \
		--before-remove dist/debian/prerm \
		--depends libspf2-2 \
		--depends libmilter1.0.1 \
		--license "Apache 2.0" \
		-m "Dolf Schimmel <dolf@transip.nl>" \
		--url "https://github.com/Freeaqingme/ClueGetter" \
		--vendor "cluegetter.net" \
		--description "Access and Auditing Milter for Postfix \n\
		 Cluegetter provides a means to have an integrated way to determine if \n\
		 a message should be accepted by Postfix. All email (metadata) and \n\
		 verdicts are stored in a database allowing for auditing." \
		--category mail \
		--config-files /etc/cluegetter/cluegetter.conf \
		--directories /var/run/cluegetter \
		.
