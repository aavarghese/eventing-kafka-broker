package v1

import (
	"testing"

	"knative.dev/pkg/webhook/resourcesemantics"
)

func TestSetDefaults(t *testing.T) {
	p := &Pod{}
	cp := p.DeepCopyObject()
	gcrd := cp.(resourcesemantics.GenericCRD)
	_ = gcrd
}
