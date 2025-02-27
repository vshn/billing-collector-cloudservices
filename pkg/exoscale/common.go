package exoscale

import (
	"encoding/base64"
	"fmt"
	egoscale "github.com/exoscale/egoscale/v3"
	"strings"
)

var Endpoints []egoscale.Endpoint = []egoscale.Endpoint{
	egoscale.CHGva2,
	egoscale.CHDk2,
	egoscale.DEFra1,
	egoscale.DEMuc1,
	egoscale.ATVie1,
	egoscale.ATVie2,
	egoscale.BGSof1,
}

const (
	// namespaceLabel represents the label used for namespace when fetching the metrics
	namespaceLabel = "crossplane.io/claim-namespace"
)

type Sourcer interface {
	GetSourceString() string
	GetCategoryString() string
}

// Aggregated contains information needed to save the metrics of the different resource types in the database
type Aggregated struct {
	Key
	Source Sourcer
	// Value represents the aggregate amount by Key of used service
	Value float64
}

// Key is the base64 key
type Key string

// NewKey creates new Key with slice of strings as inputs
func NewKey(tokens ...string) Key {
	return Key(base64.StdEncoding.EncodeToString([]byte(strings.Join(tokens, ";"))))
}

func (k *Key) String() string {
	if k == nil {
		return ""
	}
	tokens, err := k.DecodeKey()
	if err != nil {
		return ""
	}

	return fmt.Sprintf("Decoded key with tokens: %v", tokens)
}

// DecodeKey decodes Key with slice of strings as output
func (k *Key) DecodeKey() (tokens []string, err error) {
	if k == nil {
		return []string{}, fmt.Errorf("key not initialized")
	}
	decodedKey, err := base64.StdEncoding.DecodeString(string(*k))
	if err != nil {
		return []string{}, fmt.Errorf("cannot decode key %s: %w", k, err)
	}
	s := strings.Split(string(decodedKey), ";")
	return s, nil
}
