# billing-collector-cloudservices

[![Build](https://img.shields.io/github/workflow/status/vshn/billing-collector-cloudservices/Test)][build]
![Go version](https://img.shields.io/github/go-mod/go-version/vshn/billing-collector-cloudservices)
[![Version](https://img.shields.io/github/v/release/vshn/billing-collector-cloudservices)][releases]
[![GitHub downloads](https://img.shields.io/github/downloads/vshn/billing-collector-cloudservices/total)][releases]

[build]: https://github.com/vshn/billing-collector-cloudservices/actions?query=workflow%3ATest
[releases]: https://github.com/vshn/billing-collector-cloudservices/releases

Batch job to sync usage data from the Exoscale and Cloudscale API to Odoo16.

Metrics are collected based on [metered billing data flow](https://docs.central.vshn.ch/metered-billing-data-flow.html)

See the [documentation](https://kb.vshn.ch/app-catalog/reference/cloud-usage-reporting.html) for more information.

## APPUiO Cloud vs APPUiO Managed

The tool operates in 2 modes - APPUiO Cloud and APPUiO Managed. 
The mode is decided by the environment variable `APPUIO_MANAGED_SALES_ORDER`.
If the sales order is set, the tool assumes that the whole cluster is APPUiO Managed thus changing the business logic accordingly.

## Getting started for developers

In order to run this tool, you need
* Access to the Exoscale and Cloudscale accounts which has the services to be invoiced
* Access to the Kubernetes cluster which has the claims corresponding to the Exoscale services
* Access to Odoo16 where to send the metrics data

Get all this (see below), and put it all into an 'env' file:

```
export EXOSCALE_API_KEY="..."
export EXOSCALE_API_SECRET="..."
export APPUIO_MANAGED_SALES_"..."
export ODOO_OAUTH_CLIENT_SECRET="..."
export ODOO_OAUTH_TOKEN_URL="..."
export ODOO_URL="..."
export CLUSTER_ID="..."
```

Then source the env file and run the client:

```
$ . ./env
$ make build
```

Then, run one of the available commands:

* Exoscale Object Storage:
```
$ ./billing-collector-cloudservices exoscale objectstorage
```

### Create Resources in Lab Cluster to test metrics collector

You can first connect to your cluster and then create an ObjectBucket claim, for example:

```
apiVersion: appcat.vshn.io/v1
kind: ObjectBucket
metadata:
  namespace: default
  name: exoscale-bucket-lab-test-1
spec:
  parameters:
    bucketName: exoscale-bucket-lab-test-1
    region: ch-gva-2
  writeConnectionSecretToRef:
    name: bucket-connection-details
```

Once the bucket is created and `Ready`, you can run locally the command with the required env variables:
```
$ ./billing-collector-cloudservices exoscale objectstorage
```

And don't forget to delete the resource(s) you created once you're done testing.

### Exoscale API key and secret

You can get your Exoscale API key and secret from the Exoscale web UI. Be sure to select the correct project.

The token should be restricted to the 'sos' service.

### Integration tests

Integration tests create an envtest cluster and export the metrics locally. This is all automated when running:

```bash
$ make test-integration
```

To run integration tests in your IDE of choice, be sure to set build tag `integration` and the following env variables:

```bash
# path to directory where the respective go modules are installed. You can also specify the path to the local clone of the respective repositories.
EXOSCALE_CRDS_PATH="$(go list -f '{{.Dir}}' -m github.com/vshn/provider-exoscale)/package/crds)"
CLOUDSCALE_CRDS_PATH="$(go list -f '{{.Dir}}' -m github.com/vshn/provider-cloudscale)/package/crds)"

# make sure to run make target `test-integration` first to have everything setup correctly.
KUBEBUILDER_ASSETS="$(/path/to/billing-collector-cloudservices/.work/bin/setup-envtest --bin-dir "/path/to/billing-collector-cloudservices/.work/bin" use -i -p path '1.24.x!')"
```
