//go:build ruleguard

package gorules

import (
	bundle "github.com/gechr/gorules"
	"github.com/quasilyte/go-ruleguard/dsl"
)

func init() {
	dsl.ImportRules("gechr", bundle.Bundle)
}
