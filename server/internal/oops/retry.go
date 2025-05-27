package oops

type retryError struct {
	permanent bool
	err       error
}

func (e *retryError) Unwrap() error {
	return e.err
}

func (e *retryError) Error() string {
	return e.err.Error()
}

func Perm(err error) error {
	if err == nil {
		return nil
	}

	return &retryError{
		permanent: true,
		err:       err,
	}
}

func Temp(err error) error {
	if err == nil {
		return nil
	}

	return &retryError{
		permanent: false,
		err:       err,
	}
}
