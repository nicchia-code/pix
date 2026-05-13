package pix

type ExitError struct {
	Code int
	Msg  string
}

func (e ExitError) Error() string {
	return e.Msg
}

func userError(msg string) error {
	return ExitError{Code: 1, Msg: msg}
}
