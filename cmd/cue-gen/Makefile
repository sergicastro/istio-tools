mk_path := $(abspath $(lastword $(MAKEFILE_LIST)))
mk_dir  := $(dir $(mk_path))

test: build
	cd test && make clean test

build:
	go install .

ARCHS := arm64 amd64
OSS := darwin linux

BINARIES:=$(foreach binos,$(OSS),$(foreach binarch,$(ARCHS),$(mk_dir)bin/cue-gen-$(binos)-$(binarch)-static))
$(BINARIES):
	@$(call go-build,$(shell echo $(notdir $@) | cut -d"-" -f3),$(shell echo $(notdir $@) | cut -d"-" -f4))
	@echo $(notdir $@) done

define go-build
	CGO_ENABLED=0 GOOS=$(1) GOARCH=$(2) go build \
	-ldflags '-extldflags "-static"' -o $@
	chmod +x $@
endef

build-static: $(BINARIES)

clean:
	@rm -f $(BINARIES)
