package exoscale

import (
	"fmt"
	"github.com/exoscale/egoscale/v3/credentials"

	egoscale "github.com/exoscale/egoscale/v3"
)

// NewClient creates exoscale client with given access and secret keys
func NewClient(exoscaleAccessKey, exoscaleSecret string) (*egoscale.Client, error) {
	return NewClientWithOptions(exoscaleAccessKey, exoscaleSecret)
}

func NewClientWithOptions(exoscaleAccessKey string, exoscaleSecret string, options ...egoscale.ClientOpt) (*egoscale.Client, error) {
	client, err := egoscale.NewClient(credentials.NewStaticCredentials(exoscaleAccessKey, exoscaleSecret), options...)
	if err != nil {
		return nil, fmt.Errorf("cannot create Exoscale client: %w", err)
	}
	return client, nil
}
