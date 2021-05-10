# Call `make V=1` in order to print commands verbosely.
ifeq ($(V),1)
    Q =
else
    Q = @
endif

SHELL = /usr/bin/env bash -eo pipefail

# Host information
OS   := $(shell uname)
ARCH := $(shell uname -m)

# Directories
SOURCE_DIR := $(abspath $(dir $(lastword ${MAKEFILE_LIST})))
BUILD_DIR  := ${SOURCE_DIR}/_build
TOOLS_DIR  := ${BUILD_DIR}/tool

unexport GOROOT
export GOBIN    = ${BUILD_DIR}/bin
export GOCACHE ?= ${BUILD_DIR}/cache
export GOPROXY ?= https://goproxy.cn
export PATH    := ${BUILD_DIR}/bin:${PATH}

PROTOC                := ${TOOLS_DIR}/protoc/bin/protoc
PROTOC_VERSION        ?= 3.12.4
PROTOC_GEN_GO         := ${TOOLS_DIR}/protoc-gen-go
PROTOC_GEN_GO_VERSION ?= 1.3.2

# Dependency downloads
ifeq (${OS},Darwin)
	PROTOC_URL  ?= https://github.com/protocolbuffers/protobuf/releases/download/v${PROTOC_VERSION}/protoc-${PROTOC_VERSION}-osx-x86_64.zip
	PROTOC_HASH ?= 210227683a5db4a9348cd7545101d006c5829b9e823f3f067ac8539cb387519e
else ifeq (${OS},Linux)
	PROTOC_URL  ?= https://github.com/protocolbuffers/protobuf/releases/download/v${PROTOC_VERSION}/protoc-${PROTOC_VERSION}-linux-x86_64.zip
	PROTOC_HASH ?= d0d4c7a3c08d3ea9a20f94eaface12f5d46d7b023fe2057e834a4181c9e93ff3
endif

# External tools
${PROTOC_GEN_GO}: TOOL_PACKAGE = github.com/golang/protobuf/protoc-gen-go
${PROTOC_GEN_GO}: TOOL_VERSION = v${PROTOC_GEN_GO_VERSION}

# By default, intermediate targets get deleted automatically after a successful
# build. We do not want that though: there's some precious intermediate targets
# like our `*.version` targets which are required in order to determine whether
# a dependency needs to be rebuilt. By specifying `.SECONDARY`, intermediate
# targets will never get deleted automatically.
.SECONDARY:

${BUILD_DIR}:
	${Q}mkdir -p ${BUILD_DIR}
${BUILD_DIR}/bin: | ${BUILD_DIR}
	${Q}mkdir -p ${BUILD_DIR}/bin
${TOOLS_DIR}: | ${BUILD_DIR}
	${Q}mkdir -p ${TOOLS_DIR}

.PHONY: dependency-version
${TOOLS_DIR}/%.version: dependency-version | ${TOOLS_DIR}
	${Q}[ x"$$(cat "$@" 2>/dev/null)" = x"${TOOL_VERSION}" ] || >$@ echo -n "${TOOL_VERSION}"

.PHONY: proto
proto: ${PROTOC} ${PROTOC_GEN_GO}
	${Q}rm -f ${SOURCE_DIR}/example/*.pb.go
	${PROTOC} --plugin=${PROTOC_GEN_GO} -I ${SOURCE_DIR}/example --go_out=paths=source_relative,plugins=grpc:${SOURCE_DIR}/example ${SOURCE_DIR}/example/*.proto

${TOOLS_DIR}/protoc.zip: TOOL_VERSION = ${PROTOC_VERSION}
${TOOLS_DIR}/protoc.zip: ${TOOLS_DIR}/protoc.version
	${Q}if [ -z "${PROTOC_URL}" ]; then echo "Cannot generate protos on unsupported platform ${OS}" && exit 1; fi
	curl -o $@.tmp --silent --show-error -L ${PROTOC_URL}
	${Q}printf '${PROTOC_HASH}  $@.tmp' | sha256sum -c -
	${Q}mv $@.tmp $@

${PROTOC}: ${TOOLS_DIR}/protoc.zip
	${Q}rm -rf ${TOOLS_DIR}/protoc
	${Q}unzip -DD -q -d ${TOOLS_DIR}/protoc ${TOOLS_DIR}/protoc.zip

# We're using per-tool go.mod files in order to avoid conflicts in the graph in
# case we used a single go.mod file for all tools.
${TOOLS_DIR}/%/go.mod: | ${TOOLS_DIR}
	${Q}mkdir -p $(dir $@)
	${Q}cd $(dir $@) && go mod init _build

${TOOLS_DIR}/%: GOBIN = ${TOOLS_DIR}
${TOOLS_DIR}/%: ${TOOLS_DIR}/%.version ${TOOLS_DIR}/.%/go.mod
	${Q}cd ${TOOLS_DIR}/.$* && go get ${TOOL_PACKAGE}@${TOOL_VERSION}
