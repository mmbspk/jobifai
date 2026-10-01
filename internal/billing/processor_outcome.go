package billing

type ignoredEventError struct {
	code string
	msg  string
}

func (e *ignoredEventError) Error() string { return e.msg }

func errEventIgnored(code, msg string) error {
	return &ignoredEventError{code: code, msg: msg}
}

func asIgnoredEvent(err error) (code, msg string, ok bool) {
	if err == nil {
		return "", "", false
	}
	if ign, ok := err.(*ignoredEventError); ok {
		return ign.code, ign.msg, true
	}
	return "", "", false
}
