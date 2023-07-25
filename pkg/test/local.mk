###
### Integration Tests
###
setup_envtest_bin = $(go_bin)/setup-envtest

clean_targets += .envtest-clean .acr-clean

CLOUDSCALE_CRDS_PATH ?= $(shell go list -f '{{.Dir}}' -m github.com/vshn/provider-cloudscale)/package/crds
EXOSCALE_CRDS_PATH ?= $(shell go list -f '{{.Dir}}' -m github.com/vshn/provider-exoscale)/package/crds

# Prepare binary
$(setup_envtest_bin): export GOBIN = $(go_bin)
$(setup_envtest_bin): | $(go_bin)
	go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest

.PHONY: test-integration
test-integration: $(setup_envtest_bin) ## Run integration tests against code
	$(setup_envtest_bin) $(ENVTEST_ADDITIONAL_FLAGS) use '$(ENVTEST_K8S_VERSION)!'
	@chmod -R +w $(go_bin)/k8s
	export KUBEBUILDER_ASSETS="$$($(setup_envtest_bin) $(ENVTEST_ADDITIONAL_FLAGS) use -i -p path '$(ENVTEST_K8S_VERSION)!')" && \
	export CLOUDSCALE_CRDS_PATH="$(CLOUDSCALE_CRDS_PATH)" && \
	export EXOSCALE_CRDS_PATH="$(EXOSCALE_CRDS_PATH)" && \
	go test -tags=integration -coverprofile cover.out -covermode atomic ./...

.PHONY: .envtest-clean
.envtest-clean:
	rm -f $(setup_envtest_bin)
