package validator

import "strings"

type Error struct {
	Msg    string
	Fields map[string]string
}

func (e Error) Error() string {
	if len(e.Fields) > 0 {
		return join(e.Fields)
	}

	return e.Msg
}

func join(errors map[string]string) string {
	s := make([]string, 0, len(errors))
	for k, v := range errors {
		s = append(s, k+"="+v)
	}
	return strings.Join(s, ";")
}
