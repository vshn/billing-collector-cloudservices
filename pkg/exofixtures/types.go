package exofixtures

import (
	"database/sql"
	"strings"

	"github.com/appuio/appuio-cloud-reporting/pkg/db"
)

const (
	Provider = "exoscale"

	// SosType represents object storage storage type
	SosType        ObjectType = "appcat_object-storage-storage"
	QuerySos                  = string(SosType) + ":" + Provider
	DefaultUnitSos            = "GBDay"
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

const (
	queryDBaaSPostgres   = string(PostgresDBaaSType) + ":" + Provider
	queryDBaaSMysql      = string(MysqlDBaaSType) + ":" + Provider
	queryDBaaSOpensearch = string(OpensearchDBaaSType) + ":" + Provider
	queryDBaaSRedis      = string(RedisDBaaSType) + ":" + Provider
	queryDBaaSKafka      = string(KafkaDBaaSType) + ":" + Provider
	defaultUnitDBaaS     = "Instances"
)

// BillingTypes contains exoscale service types to Query billing Database types
var BillingTypes = map[string]string{
	"pg":         queryDBaaSPostgres,
	"mysql":      queryDBaaSMysql,
	"opensearch": queryDBaaSOpensearch,
	"redis":      queryDBaaSRedis,
	"kafka":      queryDBaaSKafka,
}

var DBaaS = map[ObjectType]InitConfig{
	// Postgres specific objects for billing database
	PostgresDBaaSType: {
		Products: generatePostgresProducts(),
		Discount: db.Discount{
			Source:   string(PostgresDBaaSType),
			Discount: 0,
			During:   db.InfiniteRange(),
		},
		Query: db.Query{
			Name:        queryDBaaSPostgres,
			Description: "Database Service - PostgreSQL (exoscale.com)",
			Query:       "",
			Unit:        defaultUnitDBaaS,
			During:      db.InfiniteRange(),
		},
	},

	// Mysql specific objects for billing database
	MysqlDBaaSType: {
		Products: generateMysqlProducts(),
		Discount: db.Discount{
			Source:   string(MysqlDBaaSType),
			Discount: 0,
			During:   db.InfiniteRange(),
		},
		Query: db.Query{
			Name:        queryDBaaSMysql,
			Description: "Database Service - MySQL (exoscale.com)",
			Query:       "",
			Unit:        defaultUnitDBaaS,
			During:      db.InfiniteRange(),
		},
	},

	// Opensearch specific objects for billing database
	OpensearchDBaaSType: {
		Products: generateOpensearchProducts(),
		Discount: db.Discount{
			Source:   string(OpensearchDBaaSType),
			Discount: 0,
			During:   db.InfiniteRange(),
		},
		Query: db.Query{
			Name:        queryDBaaSOpensearch,
			Description: "Database Service - Opensearch (exoscale.com)",
			Query:       "",
			Unit:        defaultUnitDBaaS,
			During:      db.InfiniteRange(),
		},
	},

	// Redis specific objects for billing database
	RedisDBaaSType: {
		Products: generateRedisProducts(),
		Discount: db.Discount{
			Source:   string(RedisDBaaSType),
			Discount: 0,
			During:   db.InfiniteRange(),
		},
		Query: db.Query{
			Name:        queryDBaaSRedis,
			Description: "Database Service - Redis (exoscale.com)",
			Query:       "",
			Unit:        defaultUnitDBaaS,
			During:      db.InfiniteRange(),
		},
	},

	// Kafka specific objects for billing database
	KafkaDBaaSType: {
		Products: generateKafkaProducts(),
		Discount: db.Discount{
			Source:   string(KafkaDBaaSType),
			Discount: 0,
			During:   db.InfiniteRange(),
		},
		Query: db.Query{
			Name:        queryDBaaSKafka,
			Description: "Database Service - Kafka (exoscale.com)",
			Query:       "",
			Unit:        defaultUnitDBaaS,
			During:      db.InfiniteRange(),
		},
	},
}

// ObjectType defines model for DBaaS types
type ObjectType string

type productDBaaS struct {
	Plan   string
	Target string
	Amount float64
}

// InitConfig is used to define and then save the initial configuration
type InitConfig struct {
	Products []*db.Product
	Discount db.Discount
	Query    db.Query
}

type DBaaSSourceString struct {
	Query        string
	Organization string
	Namespace    string
	Plan         string
}

func (ss DBaaSSourceString) GetQuery() string {
	return ss.Query
}

func (ss DBaaSSourceString) GetSourceString() string {
	return strings.Join([]string{ss.Query, ss.Organization, ss.Namespace, ss.Plan}, ":")
}

func (ss DBaaSSourceString) GetCategoryString() string {
	return Provider + ":" + ss.Namespace
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
