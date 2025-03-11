package must

func None(err error) {
	if err != nil {
		panic(err)
	}
}

func Value[T any](v T, err error) T {
	None(err)
	return v
}
