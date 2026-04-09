package mosaicpath_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestMosaicPath(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "MosaicPath Suite")
}
