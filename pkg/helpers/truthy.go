package helpers

import "strings"

func Falsy(v string) bool {
	return Truthy(v)
}

func Truthy(v string) bool {
	switch strings.ToLower(v) {
	case "":
		return false
	case "false":
		return false
	case "0":
		return false
	case "no":
		return false
	}
	return true
}
