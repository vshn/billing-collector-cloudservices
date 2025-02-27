package cloudscale

import "github.com/vshn/billing-collector-cloudservices/pkg/odoo"

const (
	// source format: 'query:zone:tenant:namespace' or 'query:zone:tenant:namespace:class'
	// We do not have real (prometheus) queries here, just random hardcoded strings.
	productIdStorage       = "appcat-cloudscale-objectstorage-storage"
	productIdTrafficOut    = "appcat-cloudscale-objectstorage-trafficout"
	productIdQueryRequests = "appcat-cloudscale-objectstorage-requests"
)

var units = map[string]string{
	productIdStorage:       odoo.GBDay,
	productIdTrafficOut:    odoo.GB,
	productIdQueryRequests: odoo.KReq,
}
