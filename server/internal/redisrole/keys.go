package redisrole

import (
	"strconv"
	"strings"

	"github.com/redis/go-redis/v9"
)

// commandsWithoutKeys are commands the guard never inspects: connection
// setup, server administration and introspection, and transaction framing.
var commandsWithoutKeys = map[string]struct{}{
	"auth":     {},
	"client":   {},
	"command":  {},
	"config":   {},
	"dbsize":   {},
	"discard":  {},
	"echo":     {},
	"exec":     {},
	"flushall": {},
	"flushdb":  {},
	"hello":    {},
	"info":     {},
	"multi":    {},
	"ping":     {},
	"quit":     {},
	"script":   {},
	"select":   {},
	"time":     {},
	"unwatch":  {},
}

// commandsWithKeyArguments take only keys after the command name.
var commandsWithKeyArguments = map[string]struct{}{
	"del":         {},
	"exists":      {},
	"mget":        {},
	"pfcount":     {},
	"sdiff":       {},
	"sdiffstore":  {},
	"sinter":      {},
	"sinterstore": {},
	"sunion":      {},
	"sunionstore": {},
	"touch":       {},
	"unlink":      {},
	"watch":       {},
}

// commandsWithTwoKeys take a source and a destination key first.
var commandsWithTwoKeys = map[string]struct{}{
	"blmove":     {},
	"brpoplpush": {},
	"copy":       {},
	"lmove":      {},
	"rename":     {},
	"renamenx":   {},
	"rpoplpush":  {},
	"smove":      {},
}

// blockingPopCommands take keys followed by a timeout.
var blockingPopCommands = map[string]struct{}{
	"blpop":    {},
	"brpop":    {},
	"bzpopmax": {},
	"bzpopmin": {},
}

// subcommandsWithKey take a subcommand and then a key; the other
// subcommands of their command address no key.
var subcommandsWithKey = map[string]map[string]struct{}{
	"memory": {"usage": {}},
	"object": {"encoding": {}, "freq": {}, "idletime": {}, "refcount": {}},
	"xinfo":  {"consumers": {}, "groups": {}, "stream": {}},
	"xgroup": {"create": {}, "destroy": {}, "setid": {}, "createconsumer": {}, "delconsumer": {}},
}

// CommandKeys returns the keys cmd addresses, as far as they can be read from
// its arguments. Keys built inside a Lua script are not visible.
func CommandKeys(cmd redis.Cmder) []string {
	return commandKeys(cmd.Args())
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
	if _, two := commandsWithTwoKeys[name]; two {
		return argStrings(args[1:min(3, len(args))])
	}
	if _, blocking := blockingPopCommands[name]; blocking {
		if len(args) < 3 {
			return nil
		}
		return argStrings(args[1 : len(args)-1])
	}
	if subcommands, ok := subcommandsWithKey[name]; ok {
		return subcommandKey(args, subcommands)
	}

	switch name {
	case "eval", "evalsha", "eval_ro", "evalsha_ro", "fcall", "fcall_ro", "blmpop", "bzmpop":
		return countedKeys(args, 2)
	case "lmpop", "zmpop", "zinter", "zunion", "zdiff", "zintercard", "sintercard":
		return countedKeys(args, 1)
	case "sort", "sort_ro":
		keys := argStrings(args[1:min(2, len(args))])
		for i := 2; i < len(args); i++ {
			option, _ := argString(args[i])
			switch strings.ToLower(option) {
			case "store":
				if i+1 < len(args) {
					keys = append(keys, argStrings(args[i+1:i+2])...)
				}
				i++
			case "get", "by":
				i++
			case "limit":
				i += 2
			}
		}
		return keys
	case "zunionstore", "zinterstore", "zdiffstore":
		return append(argStrings(args[1:min(2, len(args))]), countedKeys(args, 2)...)
	case "mset", "msetnx":
		return everyOtherArgument(args[1:])
	case "bitop":
		if len(args) < 3 {
			return nil
		}
		return argStrings(args[2:])
	case "xread":
		return streamKeys(args[1:])
	case "xreadgroup":
		return streamKeys(afterGroup(args[1:]))
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

// countedKeys reads the numkeys argument at numKeysIndex and the keys after
// it: EVAL script numkeys key ... and ZUNIONSTORE dest numkeys key ....
func countedKeys(args []any, numKeysIndex int) []string {
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

// subcommandKey returns the key of SUBCOMMAND KEY shapes, or no key for a
// subcommand that addresses none.
func subcommandKey(args []any, withKey map[string]struct{}) []string {
	if len(args) < 3 {
		return nil
	}
	subcommand, ok := argString(args[1])
	if !ok {
		return nil
	}
	if _, keyed := withKey[strings.ToLower(subcommand)]; !keyed {
		return nil
	}
	return argStrings(args[2:3])
}

// everyOtherArgument returns the keys of KEY VALUE [KEY VALUE ...] lists.
func everyOtherArgument(args []any) []string {
	keys := make([]string, 0, (len(args)+1)/2)
	for i := 0; i < len(args); i += 2 {
		if key, ok := argString(args[i]); ok {
			keys = append(keys, key)
		}
	}
	return keys
}

// afterGroup drops XREADGROUP's GROUP group consumer, whose names may be any
// word, including "streams".
func afterGroup(args []any) []any {
	const groupTokens = 3
	for i, arg := range args {
		token, ok := argString(arg)
		if ok && strings.EqualFold(token, "group") {
			return args[min(i+groupTokens, len(args)):]
		}
	}
	return args
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
// family. A SCAN without MATCH, or whose pattern starts with a glob, names no
// family and is not checked.
func scanPattern(args []any) []string {
	for i := 0; i+1 < len(args); i++ {
		token, ok := argString(args[i])
		if !ok || !strings.EqualFold(token, "match") {
			continue
		}
		pattern, ok := argString(args[i+1])
		if !ok || pattern == "" || strings.ContainsAny(pattern[:1], `*?[\`) {
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
