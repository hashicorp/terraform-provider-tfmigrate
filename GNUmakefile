default: testacc

# Run acceptance tests
.PHONY: testacc
testacc:
	TF_ACC=1 go test ./... -v $(TESTARGS) -timeout 120m



# Change these variables as necessary.
MAIN_PACKAGE_PATH := ./
BINARY_NAME := terraform-provider-tfmigrate
VERSION := 1.1.0

# ==================================================================================== #
# HELPERS
# ==================================================================================== #

## tidy: format code and tidy modfile
.PHONY: tidy
tidy:
	go fmt ./...
	go mod tidy -v

# ==================================================================================== #
# DEVELOPMENT
# ==================================================================================== #

## Add this to your ~/.zshrc or ~/.bashrc
## export PATH=$PATH:$GOPATH/bin
.PHONY: build
build:
	go build -o ${GOPATH}/bin/${BINARY_NAME} ${MAIN_PACKAGE_PATH}
	rm -f ./${BINARY_NAME}
	cp ${GOPATH}/bin/${BINARY_NAME} ${MAIN_PACKAGE_PATH}${BINARY_NAME}
	mkdir -p ${HOME}/.terraform.d/plugins/registry.terraform.io/hashicorp/tfmigrate/${VERSION}/darwin_arm64
	cp ${GOPATH}/bin/${BINARY_NAME} ${HOME}/.terraform.d/plugins/registry.terraform.io/hashicorp/tfmigrate/${VERSION}/darwin_arm64/${BINARY_NAME}
	mkdir -p ${HOME}/.terraform.d/plugin-cache/registry.terraform.io/hashicorp/tfmigrate/${VERSION}/darwin_arm64
	cp ${GOPATH}/bin/${BINARY_NAME} ${HOME}/.terraform.d/plugin-cache/registry.terraform.io/hashicorp/tfmigrate/${VERSION}/darwin_arm64/${BINARY_NAME}


.PHONY: run
run:
	rm -rf .terraform .terraform.lock.hcl terraform.tfstate terraform.tfstate.backup
	terraform init
	TF_LOG=TRACE terraform apply -auto-approve

.PHONY: runverb
runverb:
	rm -rf .terraform .terraform.lock.hcl
	TF_LOG=INFO terraform apply -auto-approve

# Variables
TFRC_FILE := provider_tfmigrate_override.tfrc
TFRC_PATH := $(CURDIR)/$(TFRC_FILE)

# Target to generate the override tfrc file
.PHONY: dev-override
dev-override:
	@echo 'provider_installation {' > $(TFRC_FILE)
	@echo '  dev_overrides {' >> $(TFRC_FILE)
	@echo '    "hashicorp/tfmigrate" = "$(CURDIR)"' >> $(TFRC_FILE)
	@echo '  }' >> $(TFRC_FILE)
	@echo '  direct {}' >> $(TFRC_FILE)
	@echo '}' >> $(TFRC_FILE)
	@echo "Generated $(TFRC_FILE) with dev override for provider tfmigrate."
	@echo "Use this file to override the provider installation during development by setting the TF_CLI_CONFIG_FILE environment variable using:"
	@echo "export TF_CLI_CONFIG_FILE=$(TFRC_PATH)"

.PHONY: help
help:
	@echo "Makefile commands:"
	@echo "  tidy          - Format code and tidy modfile"
	@echo "  build         - Build the provider binary"
	@echo "  run           - Run Terraform apply with trace logging"
	@echo "  runverb       - Run Terraform apply with info logging"
	@echo "  dev-override  - Generate a tfrc file for development override of the provider"
	@echo "  testacc       - Run acceptance tests"
	@echo "  help          - Show this help message"