package noanonymousdefercustommessage

func bad() {
	defer func() {}() // want "avoid anonymous deferred functions: use a named deferred helper instead"
}
