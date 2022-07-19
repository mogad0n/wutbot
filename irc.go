package main

import (
	"crypto/tls"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/ergochat/irc-go/ircevent"
	"github.com/ergochat/irc-go/ircmsg"
)

type empty struct{}

const (
	concurrencyLimit = 128

	IRCv3TimestampFormat = "2006-01-02T15:04:05.000Z"

	defaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/98.0.4758.81 Safari/537.36"

	replyTagName = "+draft/reply"
)

type Bot struct {
	ircevent.Connection
	TwitterBearerToken string
	Owner              string
	semaphore          chan empty
	userAgent          string
}

func (b *Bot) tryAcquireSemaphore() bool {
	select {
	case b.semaphore <- empty{}:
		return true
	default:
		return false
	}
}

func (b *Bot) releaseSemaphore() {
	<-b.semaphore
}

func (irc *Bot) checkErr(err error, message string) (fatal bool) {
	if err != nil {
		irc.Log.Printf("%s: %v", message, err)
		return true
	}
	return false
}

// Helper Functions

func (irc *Bot) handleOwnerCommand(target, command string) {
	if !strings.HasPrefix(command, irc.Nick) {
		return
	}
	command = strings.TrimPrefix(command, irc.Nick)
	command = strings.TrimPrefix(command, ":")
	f := strings.Fields(command)
	if len(f) == 0 {
		return
	}
	switch strings.ToLower(f[0]) {
	case "abuse":
		if len(f) > 1 {
			irc.Privmsg(target, fmt.Sprintf("%s isn't a real programmer", f[1]))
		}
	case "quit":
		irc.Quit()
	}
}

func (irc *Bot) sendReplyNotice(target, msgid, text string) {
	if msgid == "" {
		irc.Notice(target, text)
	} else {
		irc.SendWithTags(map[string]string{replyTagName: msgid}, "NOTICE", target, text)
	}
}

func ownerMatches(e ircmsg.Message, owner string) bool {
	if owner == "" {
		return false
	}
	if present, account := e.GetTag("account"); present && account == owner {
		return true
	}
	return false
}

func newBot() *Bot {
	// required:
	nick := os.Getenv("TITLEBOT_NICK")
	server := os.Getenv("TITLEBOT_SERVER")
	// required (comma-delimited list of channels)
	channels := os.Getenv("TITLEBOT_CHANNELS")
	// SASL is optional:
	saslLogin := os.Getenv("TITLEBOT_SASL_LOGIN")
	saslPassword := os.Getenv("TITLEBOT_SASL_PASSWORD")
	// a Twitter API key (v2-capable) is optional (if unset, Twitter support is disabled):
	twitterToken := os.Getenv("TITLEBOT_TWITTER_BEARER_TOKEN")
	// owner is optional (if unset, titlebot won't accept any owner commands)
	owner := os.Getenv("TITLEBOT_OWNER_ACCOUNT")
	// more optional settings
	version := os.Getenv("TITLEBOT_VERSION")
	if version == "" {
		version = "github.com/ergochat/irc-go"
	}
	debug := os.Getenv("TITLEBOT_DEBUG") != ""
	insecure := os.Getenv("TITLEBOT_INSECURE_SKIP_VERIFY") != ""
	userAgent := os.Getenv("TITLEBOT_USER_AGENT")
	if userAgent == "" {
		userAgent = defaultUserAgent
	}

	var tlsconf *tls.Config
	if insecure {
		tlsconf = &tls.Config{InsecureSkipVerify: true}
	}

	irc := &Bot{
		Connection: ircevent.Connection{
			Server:       server,
			Nick:         nick,
			UseTLS:       true,
			TLSConfig:    tlsconf,
			RequestCaps:  []string{"server-time", "message-tags", "account-tag"},
			SASLLogin:    saslLogin, // SASL will be enabled automatically if these are set
			SASLPassword: saslPassword,
			QuitMessage:  version,
			Debug:        debug,
		},
		TwitterBearerToken: twitterToken,
		Owner:              owner,
		userAgent:          userAgent,
		semaphore:          make(chan empty, concurrencyLimit),
	}

	irc.AddConnectCallback(func(e ircmsg.Message) {
		if botMode := irc.ISupport()["BOT"]; botMode != "" {
			irc.Send("MODE", irc.CurrentNick(), "+"+botMode)
		}
		for _, channel := range strings.Split(channels, ",") {
			irc.Join(strings.TrimSpace(channel))
		}
	})
	irc.AddCallback("PRIVMSG", func(e ircmsg.Message) {
		target, message := e.Params[0], e.Params[1]
		_, msgid := e.GetTag("msgid")
		fromOwner := ownerMatches(e, irc.Owner)
		if !strings.HasPrefix(target, "#") && !fromOwner {
			return
		}

		if fromOwner {
			irc.handleOwnerCommand(e.Params[0], message)
		} else if strings.HasPrefix(message, irc.Nick) {
			irc.sendReplyNotice(e.Params[0], msgid, "don't @ me, mortal")
		}
	})
	irc.AddCallback("INVITE", func(e ircmsg.Message) {
		fromOwner := ownerMatches(e, irc.Owner)
		if fromOwner {
			irc.Join(e.Params[1])
		}
	})

	return irc
}

func main() {
	irc := newBot()
	err := irc.Connect()
	if err != nil {
		log.Fatal(err)
	}
	irc.Loop()
}
