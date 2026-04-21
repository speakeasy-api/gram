package noanonymousdefer

func named() {}

func acceptsFunc(fn func()) {
	fn()
}

func bad() {
	defer func() {}() // want "avoid anonymous deferred functions"
}

func good() {
	defer named()
	defer acceptsFunc(func() {})
}
