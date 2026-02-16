// Package stringutil provides string manipulation utilities
package stringutil

import "strings"

func ToTitleCase(s string) string {
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		if i == 0 || s[i-1] == ' ' {
			sb.WriteString(strings.ToUpper(string(s[i])))
		} else {
			sb.WriteString(string(s[i]))
		}
	}
	return sb.String()
}
