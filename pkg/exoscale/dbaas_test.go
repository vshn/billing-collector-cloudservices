package exoscale

import (
	"context"
	"reflect"
	"testing"

	egoscale "github.com/exoscale/egoscale/v2"
	"github.com/stretchr/testify/assert"
	"github.com/vshn/exoscale-metrics-collector/pkg/exofixtures"
)

func TestDBaaS_aggregatedDBaaS(t *testing.T) {
	ctx := context.Background()

	key1 := NewKey("vshn-xyz", "hobbyist-2", string(exofixtures.PostgresDBaaSType))
	key2 := NewKey("vshn-abc", "business-128", string(exofixtures.PostgresDBaaSType))

	expectedAggregatedDBaaS := map[Key]Aggregated{
		key1: {
			Key:          key1,
			Organization: "org1",
			Value:        1,
		},
		key2: {
			Key:          key2,
			Organization: "org2",
			Value:        1,
		},
	}

	tests := map[string]struct {
		dbaasDetails            []Detail
		exoscaleDBaaS           []*egoscale.DatabaseService
		expectedAggregatedDBaaS map[Key]Aggregated
	}{
		"given DBaaS details and Exoscale DBaasS, we should get the ExpectedAggregatedDBaasS": {
			dbaasDetails: []Detail{
				{
					Organization: "org1",
					DBName:       "postgres-abc",
					Namespace:    "vshn-xyz",
					Zone:         "ch-gva-2",
				},
				{
					Organization: "org2",
					DBName:       "postgres-def",
					Namespace:    "vshn-abc",
					Zone:         "ch-gva-2",
				},
			},
			exoscaleDBaaS: []*egoscale.DatabaseService{
				{
					Name: strToPointer("postgres-abc"),
					Type: strToPointer(string(exofixtures.PostgresDBaaSType)),
					Plan: strToPointer("hobbyist-2"),
				},
				{
					Name: strToPointer("postgres-def"),
					Type: strToPointer(string(exofixtures.PostgresDBaaSType)),
					Plan: strToPointer("business-128"),
				},
			},
			expectedAggregatedDBaaS: expectedAggregatedDBaaS,
		},
		"given DBaaS details and different names in Exoscale DBaasS, we should not get the ExpectedAggregatedDBaasS": {
			dbaasDetails: []Detail{
				{
					Organization: "org1",
					DBName:       "postgres-abc",
					Namespace:    "vshn-xyz",
					Zone:         "ch-gva-2",
				},
				{
					Organization: "org2",
					DBName:       "postgres-def",
					Namespace:    "vshn-abc",
					Zone:         "ch-gva-2",
				},
			},
			exoscaleDBaaS: []*egoscale.DatabaseService{
				{
					Name: strToPointer("postgres-123"),
					Type: strToPointer(string(exofixtures.PostgresDBaaSType)),
				},
				{
					Name: strToPointer("postgres-456"),
					Type: strToPointer(string(exofixtures.PostgresDBaaSType)),
				},
			},

			expectedAggregatedDBaaS: map[Key]Aggregated{},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			aggregatedDBaaS := aggregateDBaaS(ctx, tc.exoscaleDBaaS, tc.dbaasDetails)
			assert.True(t, reflect.DeepEqual(tc.expectedAggregatedDBaaS, aggregatedDBaaS))
		})
	}
}

func strToPointer(s string) *string {
	return &s
}
