package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPassedConfig(t *testing.T) {
	t.Run("Check for parsed SKUs", func(t *testing.T) {
		monitoredSKU := parseSKUsToBeMonitored("SKU01,SKU02,SKU03")
		assert.Equal(t,3, len(monitoredSKU))
	})
}
