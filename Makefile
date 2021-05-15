## Server version
SERVER_VERSION = v2.0
## Folder content generated files
BUILD_FOLDER = ./build
PROJECT_URL  = github.com/smoothify/velero-volume-controller
## command
GO           = go
GO_MOD       = go mod
MKDIR_P      = mkdir -p

## Random Alphanumeric String
SECRET_KEY   = $(shell cat /dev/urandom | tr -dc 'a-zA-Z0-9' | fold -w 32 | head -n 1)

## UNAME
UNAME := $(shell uname)

## Helm charts
HELM_PUBLISH_FOLDER = .helm

################################################

.PHONY: all
all: build

.PHONY: pre-build
pre-build:
	$(GO_MOD) download

.PHONY: build
build: pre-build
	$(MAKE) src.build

.PHONY: clean
clean:
	$(RM) -rf $(BUILD_FOLDER)

## src/ ########################################

.PHONY: src.build
src.build:
	GO111MODULE=on $(GO) build -v -o $(BUILD_FOLDER)/velero-volume-controller

## dockerfiles/ ########################################

.PHONY: dockerfiles.build
dockerfiles.build:
	docker build --tag smoothify/velero-volume-controller:$(SERVER_VERSION) -f ./docker/Dockerfile .

## git tag version ########################################

.PHONY: push.tag
push.tag:
	@echo "Current git tag version:"$(SERVER_VERSION)
	git tag $(SERVER_VERSION)
	git push --tags

## helm chart lint ########################################

.PHONY: lint
lint:
	@ct lint

## helm chart lint ########################################
.PHONY: publish
publish:
	@mkdir -p $(HELM_PUBLISH_FOLDER)/temp $(HELM_PUBLISH_FOLDER)/docs
	@helm package -u -d $(HELM_PUBLISH_FOLDER)/temp charts/velero-volume-controller
	@helm repo index --debug --url=https://smoothify.github.io/velero-volume-controller --merge $(HELM_PUBLISH_FOLDER)/docs/index.yaml $(HELM_PUBLISH_FOLDER)/temp
	@mv $(HELM_PUBLISH_FOLDER)/temp/velero-volume-controller*.tgz $(HELM_PUBLISH_FOLDER)/docs
	@mv $(HELM_PUBLISH_FOLDER)/temp/index.yaml $(HELM_PUBLISH_FOLDER)/docs/index.yaml
	@rm -rf $(HELM_PUBLISH_FOLDER)/temp