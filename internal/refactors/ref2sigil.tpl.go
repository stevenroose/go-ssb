//go:build none
// +build none

package p

import (
	refs "github.com/ssbc/go-ssb-refs"
)

func before(r refs.MessageRef) string {
	return r.Sigil()
}

func after(r refs.MessageRef) string {
	return r.String()
}
