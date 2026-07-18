//go:build ruleguard

package gorules

import (
	format "github.com/gechr/gorules/format"
	"github.com/quasilyte/go-ruleguard/dsl"
)

func init() {
	dsl.ImportRules("gechr", format.Bundle)
}
