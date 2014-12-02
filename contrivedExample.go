package main

import (
	"fmt"
	"github.com/kd5pbo/minimalirc"
	"regexp"
)

func main() {

	/* Make an IRC struct */
	irc := minimalirc.New("irc.freenode.net", 7000, true, "", "jbond",
		"james", "James Bond")

	/* Set some settings */
	irc.IdNick = "jbond"
	irc.IdPass = "iloveturtles"
	irc.Channel = "#mi5"
	irc.Pongs = true

	/* Connect to the server */
	if err := irc.Connect(); nil != err {
		fmt.Printf("Failed to connect to server: %v\n", err)
		return
	}
	fmt.Printf("Connected to server.\n")

	secretMessageRE := regexp.MustCompile(
		`:(\S+) PRIVMSG #mi5 :SECRET MESSAGE: (.*)`)
	/* Wait for a secret message */
	for {
		line, ok := <-irc.C
		if !ok {
			err := <-irc.E
			fmt.Printf("Error reading from server: %v\n", err)
			return
		}
		if g := secretMessageRE.FindStringSubmatch(line); 3 == len(g) {
			fmt.Printf("Secret message from %v: %v\n", g[1], g[2])
		}
	}

}
