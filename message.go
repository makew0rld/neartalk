package main

// This file deals with messages coming from or going to the web UI.
// The web UI uses htmx (htmx.org) and so HTML is passed over the websocket for
// updates.
// Like with any chat service, messages coming in have to sanitized.
// This file also deals with rendering any kinds of special messages, like red
// for errors.

import (
	"fmt"
	"html"
	"regexp"
	"strings"
	"time"

	"github.com/rivo/uniseg"
	"golang.org/x/text/unicode/norm"
)

const maxNickLen = 30
const maxMsgTextLen = 512

// URL Regex
// Source:
// John Gruber has a blog post: https://daringfireball.net/2010/07/improved_regex_for_matching_urls
// That links to this gist: https://gist.github.com/gruber/249502
// I modified the regex slightly for Go (\x60 instead of `)
// I also changed it so it wouldn't recognize non-URLs like "bit.com/test"
// I also made the protocol required
// I applied the change mention in this comment:
// https://gist.github.com/gruber/249502#gistcomment-1381560
// That way magnet links and similar are picked up
var urlRe = regexp.MustCompile(`(?i)\b(?:[a-z][\w.+-]+:(?:/{1,3}|[?+]?[a-z0-9%]))(?:[^\s()<>]+|\(([^\s()<>]+|(\([^\s()<>]+\)))*\))+(?:\(([^\s()<>]+|(\([^\s()<>]+\)))*\)|[^\s\x60!()\[\]{};:'".,<>?«»“”‘’])`)

// Sending this through the websocket to htmx clears whatever message was
// written in the input field. This is used to clear the field after the user
// sends a message.
const clearInputFieldMsg = `<input name="message" id="message-input" type="text" />`

// createChatMsg takes the message from a user and returns HTML
// that can be sent over websocket to the htmx web UI.
// It returns two messages, one for the author, and one for everyone else.
// It will return empty strings if the provided msg is considered invalid.
func createChatMsg(m msg) (string, string) {
	sanitizedMsgText := renderMsgText(m.text)
	if !isMsgTextValid(sanitizedMsgText) {
		return "", ""
	}
	ts := m.when.UTC().Format(time.RFC3339)
	author := fmt.Sprintf(
		// Add message to log
		`<tbody id="message-table-tbody" hx-swap-oob="beforeend">
			<tr><td>%s</td><td class="my-nick">%s</td><td class="my-msg">%s</td></tr>
		</tbody>`,
		ts, m.nick, sanitizedMsgText, // nick is already sanitized
	)
	nonAuthor := fmt.Sprintf(
		// Add message to log
		`<tbody id="message-table-tbody" hx-swap-oob="beforeend">
			<tr><td>%s</td><td>%s</td><td>%s</td></tr>
		</tbody>`,
		ts, m.nick, sanitizedMsgText,
	)
	return author, nonAuthor
}

func isMsgTextValid(s string) bool {
	return s != ""
}

// createUserListMsg creates HTML that can replace the current user list.
// It assume the nicknames provided are already HTML escaped.
func createUserListMsg(nicks []string) string {
	var b strings.Builder
	b.WriteString(`<div id="users-list">`)
	for i := range nicks {
		b.WriteString(fmt.Sprintf(`<p>%s</p>`, nicks[i]))
	}
	b.WriteString(`</div>`)
	b.WriteString(fmt.Sprintf(`<p id="users-header-p" class="bold">Users (%d)</p>`, len(nicks)))
	return b.String()
}

// createSpecialMsg creates a message not from any specific user, that has a
// CSS class. This can be used for error messages, or notifications.
func createSpecialMsg(text string, class string) string {
	var ts string
	if class == "notif" {
		// Notification messages are timestamped
		ts = time.Now().UTC().Format(time.RFC3339)
	}
	return fmt.Sprintf(
		// Add message to log
		`<tbody id="message-table-tbody" hx-swap-oob="beforeend">
			<tr class="special-msg"><td>%s</td><td></td><td class="%s">%s</td></tr>
		</tbody>`,
		ts, class, html.EscapeString(text),
	)
}

// createJoinMsg creates a msg struct that can be sent to a chat room when a client joins.
func createJoinMsg(c *client, nicks []string) msg {
	return msg{
		raw: createSpecialMsg(fmt.Sprintf("%s has joined", c.nick), "notif") +
			createUserListMsg(nicks),
		when: time.Now(),
	}
}

// createLeaveMsg creates a msg struct that can be sent to a chat room when a client leaves.
func createLeaveMsg(c *client, nicks []string) msg {
	return msg{
		raw: createSpecialMsg(fmt.Sprintf("%s has left", c.nick), "notif") +
			createUserListMsg(nicks),
		when: time.Now(),
	}
}

func sanitizeNick(nick string) string {
	nick = strings.ToValidUTF8(nick, "\uFFFD")
	nick = strings.TrimSpace(nick)
	// Unicode normalization, to prevent look-alike nicknames
	nick = norm.NFC.String(nick)

	// Truncate by graphemes instead of runes, so multi-rune things like flags work
	g := uniseg.NewGraphemes(nick)
	i := 0
	nick = ""
	for g.Next() && i < maxNickLen {
		nick += g.Str()
		i++
	}

	nick = html.EscapeString(nick)
	return nick
}

func renderMsgText(text string) string {
	text = strings.ToValidUTF8(text, "\uFFFD")
	text = strings.TrimSpace(text)

	// TODO: is this too slow?
	g := uniseg.NewGraphemes(text)
	i := 0
	var b strings.Builder
	for g.Next() && i < maxMsgTextLen {
		b.Write(g.Bytes())
		i++
	}
	text = b.String()
	text = html.EscapeString(text)

	// Linkify URLs
	text = urlRe.ReplaceAllStringFunc(text, func(urlText string) string {
		return fmt.Sprintf(`<a href="%s" target="_blank" rel="noopener noreferrer">%s</a>`, urlText, urlText)
	})

	return text
}

// Message handlers

// handleMsg takes a msg and performs the appropriate action.
// This may involve sending a message back to the author. If a message should
// sent to all chat room clients, handleMsg returns a two strings, one to send
// to the author, and another to send to everyone else.
// Otherwise empty strings are returned.
func (cr *chatRoom) handleMsg(m msg) (string, string) {
	cr.clientsMu.Lock()
	defer cr.clientsMu.Unlock()

	if m.raw != "" {
		// Message is already rendered
		return m.raw, m.raw
	}

	if strings.HasPrefix(m.text, "/nick ") && len(m.text) > len("/nick ") {
		newNick := sanitizeNick(m.text[len("/nick "):])
		if newNick == "" {
			// Empty nickname, invalid
			m.author.sendText(createSpecialMsg("Nickname cannot be empty", "error"))
			return "", ""
		}
		if cr.nickInUse(newNick) {
			m.author.sendText(createSpecialMsg("That nickname is already in use", "error"))
			return "", ""
		}
		oldNick := m.author.nick
		m.author.nick = newNick
		// Tell everyone about name change, and update user list
		s := createSpecialMsg(
			fmt.Sprintf("%s is now known as %s", oldNick, newNick), "notif",
		) +
			createUserListMsg(cr.nicks())
		return s, s
	}

	// Regular message
	cr.whenLastMsg = m.when
	return createChatMsg(m)
}
