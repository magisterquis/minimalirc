package minimalirc

import (
	"bufio"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/textproto"
	"strings"
	"time"
	"unicode"
)

/*
 * minimalirc.go
 * small library to connect to an IRC server
 * by J. Stuart McMurray
 * created 20141130
 * last modified 20141201
 *
 * The MIT License (MIT)
 *
 * Copyright (c) 2014 J. Stuart McMurray
 *
 * Permission is hereby granted, free of charge, to any person obtaining a copy
 * of this software and associated documentation files (the "Software"), to deal
 * in the Software without restriction, including without limitation the rights
 * to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
 * copies of the Software, and to permit persons to whom the Software is
 * furnished to do so, subject to the following conditions:
 *
 * The above copyright notice and this permission notice shall be included in all
 * copies or substantial portions of the Software.
 *
 * THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
 * IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
 * FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
 * AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
 * LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
 * OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
 * SOFTWARE.
 */

/* IRC represents a connection to an IRC server via OpenSSL */
type IRC struct {
	r       *textproto.Reader /* Reads messages from server */
	w       *textproto.Writer /* Writes messages to server */
	C       <-chan string     /* Messages from R are sent here */
	c       chan string       /* Sendable, closable C */
	E       <-chan error      /* Receives an error before close(C) */
	e       chan error        /* Sendable E */
	S       net.Conn          /* Represents the connection to the server */
	Msglen  int               /* Size of an IRC message */
	Default string            /* Default target for privmsgs */
	rng     *rand.Rand        /* Random number generator */
	snick   string            /* The server's idea of our nick */

	/* Configs and defauls.  These may be changed at any time. */
	Host          string /* Host to which to connect */
	Port          uint16 /* Port to which to connect */
	Ssl           bool   /* True to use SSL/TLS */
	Hostname      string /* Hostname to verify on server's certificate */
	Nick          string /* For NICK */
	Username      string /* For USER */
	Realname      string /* For USER */
	IdNick        string /* To auth to NickServ */
	IdPass        string /* To auth to NickServ */
	Channel       string /* For JOIN */
	Chanpass      string /* For JOIN */
	Txp           string /* Prefix for logging sent messages */
	Rxp           string /* Prefix for logging received messages */
	Pongs         bool   /* Automatic ping responses */
	RandomNumbers bool   /* Append random numbers to the nick */
	QuitMessage   string /* Message to send when the client QUITs */
}

// New allocates, initializes, and returns a pointer to a new IRC struct.  hostname will be ignored if ssl is false, or assumed to be the same as host if it is the empty string and ssl is true.
func New(host string, port uint16, ssl bool, hostname string,
	nick, username, realname string) *IRC {
	/* Struct to return */
	i := &IRC{}
	/* Random number generator */
	i.rng = rand.New(rand.NewSource(time.Now().Unix()))
	/* Default max message length */
	i.Msglen = 467
	/* I/O channels */
	i.c = make(chan string)
	i.C = i.c
	i.e = make(chan error, 1)
	i.E = i.e
	i.Host = host
	i.Port = port
	i.Ssl = ssl
	if i.Ssl && "" == hostname {
		hostname = host
	}
	i.Hostname = hostname
	i.Nick = nick
	i.Username = username
	i.Realname = realname

	return i
}

// Connect connects to the server, and calls Handshake().  After connect returns, messages sent by the IRC server will be available on i.C.  If i.Rxp is set, received messages from the server will be logged via log.Printf prefixed by i.Rxp, separated by a space.  If an error is encountered reading messages from the IRC server, i.C will be closed and the error will be sent on i.E.  i.S represents the connection to the server.
func (i *IRC) Connect() error {
	/* Dial the server */
	h := net.JoinHostPort(i.Host, fmt.Sprintf("%v", i.Port))
	if i.Ssl { /* SSL requested */
		var err error
		i.S, err = tls.Dial("tcp", h,
			&tls.Config{ServerName: i.Hostname})
		if nil != err {
			return errors.New(fmt.Sprintf("unable to make ssl "+
				"connection to %v: %v", h, err))
		}
	} else { /* Plaintext connection */
		var err error
		i.S, err = net.Dial("tcp", h)
		if nil != err {
			return errors.New(fmt.Sprintf("unable to make "+
				"plaintext connection to %v: %v", h, err))
		}
	}

	/* Make a reader and a writer */
	i.r = textproto.NewReader(bufio.NewReader(i.S))
	i.w = textproto.NewWriter(bufio.NewWriter(i.S))

	/* Send nick and user */
	if err := i.Handshake(); nil != err {
		return errors.New(fmt.Sprintf("unable to handshake: %v", err))
	}

	/* Start reads from server into channel */
	go func() {
		for {
			/* Get a line from the reader */
			line, err := i.r.ReadLine()
			/* Close the channel on error */
			if nil != err {
				i.e <- err
				close(i.c)
			}
			/* Log the line if needed */
			if "" != i.Rxp {
				log.Printf("%v %v", i.Rxp, line)
			}
			/* Handle pings if desired */
			if i.Pongs && strings.HasPrefix(strings.ToLower(line),
				"ping ") {
				/* Try to send pong */
				err := i.PrintfLine("PONG %v",
					strings.SplitN(line, " ", 2)[1])
				/* A send error is as bad as a read error */
				if nil != err {
					i.e <- err
					close(i.c)
				}
			}
			/* Maybe get a nick */
			parts := strings.SplitN(line, " ", 4)
			/* If the 2nd bit is a 3-digit number, the 3rd bit is
			our nick */
			if 4 == len(parts) {
				n := []rune(parts[1])
				if 3 == len(n) &&
					unicode.IsNumber(n[0]) &&
					unicode.IsDigit(n[1]) &&
					unicode.IsDigit(n[2]) {
					i.snick = parts[2]
				}
			}

			/* Send out the line */
			i.c <- line
		}
	}()
	return nil
}

// ID sets the nick and user from the values in i, and sends a NICK command without any parameters (to get an easy-to-parse response with the nick as the server knows it).  If i.Nick, i.Username or i.Realname are the empty string, this is a no-op.
func (i *IRC) ID() error {
	if "" == i.Nick || "" == i.Username || "" == i.Realname {
		return nil
	}
	/* Add some numbers to the nick */
	nick := i.Nick
	if i.RandomNumbers {
		nick = fmt.Sprintf("%v-%v", nick, i.rng.Int63())
	}
	/* Iterate over the commands to send */
	for _, line := range []string{
		fmt.Sprintf("NICK :%v", nick),
		fmt.Sprintf("USER %v x x :%v", i.Username, i.Realname),
		"NICK",
	} {
		/* Try to send the line */
		if err := i.PrintfLine(line); nil != err {
			return errors.New(fmt.Sprintf("error sending ID "+
				"line %v: %v", line, err))
		}
	}
	return nil
}

// Auth authenticates to NickServ with the values in i.  If either i.IdNick or i.IdPass are the empty string, this is a no-op.
func (i *IRC) Auth() error {
	/* Don't auth with blank creds */
	if "" == i.IdNick || "" == i.IdPass {
		return nil
	}
	l := fmt.Sprintf("PRIVMSG NickServ :identify %v %v", i.IdNick,
		i.IdPass)
	if err := i.PrintfLine(l); nil != err {
		return errors.New(fmt.Sprintf("error authenticating to "+
			"services: %v", err))
	}
	return nil
}

// Join joins the channel with the optional password (which may be the empty string).  If the channel is the empty string, the value from i.Channel and i.Chanpass will be used.  If channel and i.Channel are both the empty string, this is a no-op.
func (i *IRC) Join(channel, pass string) error {
	/* If not specified, try the channel in i */
	if "" == channel {
		channel = i.Channel
		pass = i.Chanpass
	}
	/* If still no channel, no-op */
	if "" == channel {
		return nil
	}
	l := fmt.Sprintf("JOIN %v %v", channel, pass)
	if err := i.PrintfLine(l); nil != err {
		return errors.New(fmt.Sprintf("error joining %v: %v",
			channel, err))
	}
	return nil
}

// Handshake is a shorthand for ID, Auth, and Join, in that order, using the values in i.
func (i *IRC) Handshake() error {
	/* Set nick and user */
	if err := i.ID(); nil != err {
		return errors.New(fmt.Sprintf("handshake error (ID): %v", err))
	}
	/* Auth to services */
	if err := i.Auth(); err != nil {
		return errors.New(fmt.Sprintf("handshake error (Auth): %v",
			err))
	}
	/* Join the channel */
	if err := i.Join("", ""); err != nil {
		return errors.New(fmt.Sprintf("handshake error (Join): %v",
			err))
	}
	return nil
}

// PrintfLine sends the formatted string to the IRC server.  The message should be a raw IRC protocol message (like WHOIS or CAP).  It is not wrapped in PRIVMSG or anything else.  For PRIVMSGs, see Privmsg  .If i.Txp is not the empty string, successfully sent lines will be logged via log.Printf() prefixed by i.Txp, separated by a space.  Note that all the functions used to send protocol messages use PrintfLine.
func (i *IRC) PrintfLine(f string, args ...interface{}) error {
	/* Form the line into a string */
	line := fmt.Sprintf(f, args...)
	/* Try to send the line */
	if err := i.w.PrintfLine(line); err != nil {
		return err
	}
	/* Log if desired */
	if "" != i.Txp {
		log.Printf("%v %v", i.Txp, line)
	}
	return nil
}

// Target returns a target suitable for use in Privmsg, or "" if there is none.
func (i *IRC) target(target string) string {
	/* Use the default target if none was given */
	if "" == target {
		target = i.Default
	}
	/* If no default, use channel */
	if "" == target {
		target = i.Channel
	}
	/* Nop if no default target */
	if "" == target {
		return ""
	}
	return target
}

// Privmsg sends a PRIVMSG to the target, which may be a nick or a channel.  If the target is an empty string, the message will be sent to i.Target, unless that is also an empty string, in which case nothing is sent.
func (i *IRC) Privmsg(msg, target string) error {
	/* Get the target */
	t := i.target(target)
	if "" == t {
		return nil
	}
	/* Send the message */
	return i.PrintfLine("PRIVMSG %v :%v", t, msg)
}

// PrivmsgSize returns the length of the message that can be shoved into a PRIVMSG to the target.  i.Msglen may be changed to override the default size of an IRC message (467 bytes, determined experimentally on freenode, 510 should be it, though).  See Privmsg for the meaning of target.
func (i *IRC) PrivmsgSize(target string) int {
	/* Get the target */
	t := i.target(target)
	if "" == t {
		return -1
	}
	return i.Msglen - len([]byte(fmt.Sprintf("PRIVMSG %v :", t)))
}

// Nick returns a guess as to what the server thinks the nick is.  This is handy for servers that truncate nicks when RandomNumbers is true.  This is, however, only a guess (albiet a good one).  It should be called after setting the nick with Nick() or Handshake().  Note this is based on passive inspection of received messagess, which requires reading due to the read channel being unbuffered. */
func (i *IRC) SNick() string {
	return i.snick
}

// Quit sends a QUIT command to the IRC server, with the optional msg as the quit message and closes the connection if the send succeeds.  If msg is the empty string, i.QuitMessage will be used, unless it's also the empty string, in which case no message is sent with the QUIT command.
func (i *IRC) Quit(msg string) error {
	/* Use the stored message if msg is empty */
	if "" == msg && "" != i.QuitMessage {
		msg = i.QuitMessage
	}
	/* Make the message protocolish */
	if "" != msg {
		msg = " :" + msg
	}
	/* Send the quit message */
	if err := i.PrintfLine("QUIT%v", msg); nil != err {
		return err
	}
	/* Close the connection */
	if err := i.S.Close(); nil != err {
		return err
	}

	return nil
}
