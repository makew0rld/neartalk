package main

import (
	"fmt"
	"math/rand"
	"strings"

	"github.com/makeworld-the-better-one/neartalk/data"
)

// genNick returns a new random nickname.
func genNick() string {
	adjective := data.Adjectives[rand.Intn(len(data.Adjectives))]
	animal := data.Animals[rand.Intn(len(data.Animals))]

	// Convert to CamelCase
	return strings.ReplaceAll(
		strings.Title(fmt.Sprintf("%s %s", adjective, animal)),
		" ", "",
	)
}
