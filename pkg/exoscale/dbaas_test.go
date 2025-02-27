package exoscale

import (
	"context"
	"testing"
	"time"

	egoscale "github.com/exoscale/egoscale/v3"
	"github.com/stretchr/testify/assert"
	"github.com/vshn/billing-collector-cloudservices/pkg/exofixtures"
	"github.com/vshn/billing-collector-cloudservices/pkg/log"
	"github.com/vshn/billing-collector-cloudservices/pkg/odoo"
)

func TestDBaaS_aggregatedDBaaS(t *testing.T) {
	ctx := getTestContext(t)

	location, _ := time.LoadLocation("Europe/Zurich")

	now := time.Now().In(location)
	record1 := odoo.OdooMeteredBillingRecord{
		ProductID:            "appcat-exoscale-v2-pg-hobbyist-2",
		InstanceID:           "ch-gva-2/postgres-abc",
		ItemDescription:      "postgres-abc",
		ItemGroupDescription: "APPUiO Managed - Cluster: c-test1 / Namespace: vshn-xyz",
		SalesOrder:           "1234",
		UnitID:               "",
		ConsumedUnits:        1,
		TimeRange: odoo.TimeRange{
			From: time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), 0, 0, 0, now.Location()).In(time.UTC),
			To:   time.Date(now.Year(), now.Month(), now.Day(), now.Hour()+1, 0, 0, 0, now.Location()).In(time.UTC),
		},
	}
	record2 := odoo.OdooMeteredBillingRecord{
		ProductID:            "appcat-exoscale-v2-pg-business-128",
		InstanceID:           "ch-gva-2/postgres-def",
		ItemDescription:      "postgres-def",
		ItemGroupDescription: "APPUiO Managed - Cluster: c-test1 / Namespace: vshn-uvw",
		SalesOrder:           "1234",
		UnitID:               "",
		ConsumedUnits:        1,
		TimeRange: odoo.TimeRange{
			From: time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), 0, 0, 0, now.Location()).In(time.UTC),
			To:   time.Date(now.Year(), now.Month(), now.Day(), now.Hour()+1, 0, 0, 0, now.Location()).In(time.UTC),
		},
	}

	expectedAggregatedOdooRecords := []odoo.OdooMeteredBillingRecord{record1, record2}

	tests := map[string]struct {
		dbaasDetails                  []Detail
		exoscaleDBaaS                 []egoscale.DBAASServiceCommon
		expectedAggregatedOdooRecords []odoo.OdooMeteredBillingRecord
	}{
		"given DBaaS details and Exoscale DBaasS, we should get the ExpectedAggregatedDBaasS": {
			dbaasDetails: []Detail{
				{
					Organization: "org1",
					DBName:       "postgres-abc",
					Namespace:    "vshn-xyz",
					Zone:         "ch-gva-2",
					Kind:         "PostgreSQLList",
				},
				{
					Organization: "org2",
					DBName:       "postgres-def",
					Namespace:    "vshn-uvw",
					Zone:         "ch-gva-2",
					Kind:         "PostgreSQLList",
				},
			},
			exoscaleDBaaS: []egoscale.DBAASServiceCommon{
				{
					Name: "postgres-abc",
					Type: egoscale.DBAASServiceTypeName(exofixtures.PostgresDBaaSType),
					Plan: "hobbyist-2",
				},
				{
					Name: "postgres-def",
					Type: egoscale.DBAASServiceTypeName(exofixtures.PostgresDBaaSType),
					Plan: "business-128",
				},
			},
			expectedAggregatedOdooRecords: expectedAggregatedOdooRecords,
		},
		"given DBaaS details and different names in Exoscale DBaasS, we should not get the ExpectedAggregatedDBaasS": {
			dbaasDetails: []Detail{
				{
					Organization: "org1",
					DBName:       "postgres-abc",
					Namespace:    "vshn-xyz",
					Zone:         "ch-gva-2",
					Kind:         "PostgreSQLList",
				},
				{
					Organization: "org2",
					DBName:       "postgres-def",
					Namespace:    "vshn-abc",
					Zone:         "ch-gva-2",
					Kind:         "PostgreSQLList",
				},
			},
			exoscaleDBaaS: []egoscale.DBAASServiceCommon{
				{
					Name: "postgres-123",
					Type: egoscale.DBAASServiceTypeName(exofixtures.PostgresDBaaSType),
				},
				{
					Name: "postgres-456",
					Type: egoscale.DBAASServiceTypeName(exofixtures.PostgresDBaaSType),
				},
			},

			expectedAggregatedOdooRecords: []odoo.OdooMeteredBillingRecord{},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			ds, _ := NewDBaaS(nil, nil, nil, 1, "1234", "c-test1", "", map[string]string{})
			aggregatedOdooRecords, err := ds.AggregateDBaaS(ctx, tc.exoscaleDBaaS, tc.dbaasDetails)
			assert.NoError(t, err)
			assert.Equal(t, tc.expectedAggregatedOdooRecords, aggregatedOdooRecords)
		})
	}
}

func getTestContext(t assert.TestingT) context.Context {
	logger, err := log.NewLogger("test", time.Now().String(), 1, "console")
	assert.NoError(t, err, "cannot create logger")
	ctx := log.NewLoggingContext(context.Background(), logger)
	return ctx
}
