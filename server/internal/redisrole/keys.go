package redisrole

import (
	"strconv"
	"strings"
)

// commandsWithoutKeys are commands the guard never inspects: connection
// setup, server introspection and transaction framing.
var commandsWithoutKeys = map[string]struct{}{
	"auth":    {},
	"client":  {},
	"command": {},
	"discard": {},
	"echo":    {},
	"exec":    {},
	"hello":   {},
	"info":    {},
	"multi":   {},
	"ping":    {},
	"quit":    {},
	"script":  {},
	"select":  {},
	"unwatch": {},
}

// commandsWithKeyArguments take only keys after the command name.
var commandsWithKeyArguments = map[string]struct{}{
	"del":    {},
	"exists": {},
	"mget":   {},
	"touch":  {},
	"unlink": {},
	"watch":  {},
}

// commandKeys returns the keys a command addresses, as far as they can be
// read from its arguments. Keys built inside a Lua script are not visible.
func commandKeys(args []any) []string {
	if len(args) == 0 {
		return nil
	}
	name, ok := argString(args[0])
	if !ok {
		return nil
	}
	name = strings.ToLower(name)

	if _, skip := commandsWithoutKeys[name]; skip {
		return nil
	}
	if _, all := commandsWithKeyArguments[name]; all {
		return argStrings(args[1:])
	}

	switch name {
	case "eval", "evalsha", "eval_ro", "evalsha_ro", "fcall", "fcall_ro":
		return scriptKeys(args)
	case "xread", "xreadgroup":
		return streamKeys(args)
	case "scan":
		return scanPattern(args)
	}

	if len(args) < 2 {
		return nil
	}
	key, ok := argString(args[1])
	if !ok {
		return nil
	}
	return []string{key}
}

// scriptKeys reads the numkeys argument of EVAL-style commands:
// EVAL script numkeys key [key ...] arg [arg ...].
func scriptKeys(args []any) []string {
	const numKeysIndex = 2
	if len(args) <= numKeysIndex {
		return nil
	}
	numKeys, ok := argInt(args[numKeysIndex])
	if !ok || numKeys <= 0 {
		return nil
	}
	end := min(numKeysIndex+1+numKeys, len(args))
	return argStrings(args[numKeysIndex+1 : end])
}

// streamKeys returns the stream names of XREAD and XREADGROUP: the first half
// of the arguments after STREAMS (the second half are the ids).
func streamKeys(args []any) []string {
	for i, arg := range args {
		token, ok := argString(arg)
		if !ok || !strings.EqualFold(token, "streams") {
			continue
		}
		rest := args[i+1:]
		return argStrings(rest[:len(rest)/2])
	}
	return nil
}

// scanPattern returns the MATCH pattern of a SCAN. Matching is on the head of
// the pattern, so a family prefix followed by a glob classifies as that
// family. A SCAN without MATCH addresses no family and is not checked.
func scanPattern(args []any) []string {
	for i := 0; i+1 < len(args); i++ {
		token, ok := argString(args[i])
		if !ok || !strings.EqualFold(token, "match") {
			continue
		}
		pattern, ok := argString(args[i+1])
		if !ok {
			return nil
		}
		return []string{pattern}
	}
	return nil
}

func argStrings(args []any) []string {
	keys := make([]string, 0, len(args))
	for _, arg := range args {
		if key, ok := argString(arg); ok {
			keys = append(keys, key)
		}
	}
	return keys
}

func argString(arg any) (string, bool) {
	switch v := arg.(type) {
	case string:
		return v, true
	case []byte:
		return string(v), true
	default:
		return "", false
	}
}

func argInt(arg any) (int, bool) {
	switch v := arg.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case string:
		n, err := strconv.Atoi(v)
		if err != nil {
			return 0, false
		}
		return n, true
	default:
		return 0, false
	}
}
