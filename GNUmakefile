default: testacc

# Run acceptance tests
.PHONY: testacc
testacc:
	TF_ACC=1 go test ./... -v $(TESTARGS) -timeout 120m



# Change these variables as necessary.
MAIN_PACKAGE_PATH := ./
BINARY_NAME := terraform-provider-tfmigrate

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
	mkdir -p ${HOME}/.terraform.d/plugins/registry.terraform.io/hashicorp/tfmigrate/0.1.0/darwin_arm64
	cp ${GOPATH}/bin/${BINARY_NAME} ${HOME}/.terraform.d/plugins/registry.terraform.io/hashicorp/tfmigrate/0.1.0/darwin_arm64/${BINARY_NAME}

.PHONY: run
run:
	rm -rf .terraform .terraform.lock.hcl terraform.tfstate terraform.tfstate.backup
	terraform init
	TF_LOG=TRACE terraform apply -auto-approve

.PHONY: runverb
runverb:
	rm -rf .terraform .terraform.lock.hcl
	TF_LOG=INFO terraform apply -auto-approve
