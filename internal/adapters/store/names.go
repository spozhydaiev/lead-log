package store

import "strings"

var apostropheReplacer = strings.NewReplacer(
	"’", "'",
	"‘", "'",
	"`", "'",
	"´", "'",
	"ʼ", "'",
	"ʹ", "'",
)

func NormalizePersonName(name string) string {
	name = apostropheReplacer.Replace(strings.TrimSpace(name))
	return strings.ToLower(strings.Join(strings.Fields(name), " "))
}
