package exofixtures

import (
	"database/sql"

	"github.com/appuio/appuio-cloud-reporting/pkg/db"
)

const (
	Provider = "exoscale"

	// SosType represents object storage storage type
	SosType  ObjectType = "appcat_object-storage-storage"
	QuerySos            = string(SosType) + ":" + Provider
)

var ObjectStorage = InitConfig{
	Products: []*db.Product{
		{
			Source: QuerySos,
			Target: sql.NullString{String: "1402", Valid: true},
			Amount: 0.000726,
			Unit:   "GBDay",
			During: db.InfiniteRange(),
		},
	},
	Discount: db.Discount{
		Source:   string(SosType),
		Discount: 0,
		During:   db.InfiniteRange(),
	},
	Query: db.Query{
		Name:        QuerySos,
		Description: "Object Storage - Storage (exoscale.com)",
		Query:       "",
		Unit:        "GBDay",
		During:      db.InfiniteRange(),
	},
}

// ObjectType defines model for billed object types
type ObjectType string

// InitConfig is used to define and then save the initial configuration
type InitConfig struct {
	Products []*db.Product
	Discount db.Discount
	Query    db.Query
}

type SOSSourceString struct {
	Namespace    string
	Organization string
}

func (ss SOSSourceString) GetSourceString() string {
	return string(SosType) + ":" + Provider + ":" + ss.Organization + ":" + ss.Namespace
}

func (ss SOSSourceString) GetCategoryString() string {
	return Provider + ":" + ss.Namespace
}
