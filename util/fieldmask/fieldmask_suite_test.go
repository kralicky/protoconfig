package fieldmask_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestFieldmask(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Fieldmask Suite")
}
