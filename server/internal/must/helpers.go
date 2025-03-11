package must

func Nil(err error) {
	if err != nil {
		panic(err)
	}
}

func Value[T any](v T, err error) T {
	Nil(err)
	return v
}
