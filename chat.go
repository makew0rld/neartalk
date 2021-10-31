package main

// This file handles the majority of the application.
// Chat messages, HTTP server, websocket handling, etc.
//
// It was adapted from some chat example code by nhooyr:
// https://github.com/nhooyr/websocket/tree/b6adc4bc5c001d513d1604ec9efd97e73e1d082a/examples/chat
// That code is under the MIT license, so there are no licensing issues with it
// being adapted for here. Note it was adapted, and not directly copied. The
// example was used as a starting point.

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

// clientMsgBuffer controls the max number
// of messages that can be queued for a client
// before it is kicked.
const clientMsgBuffer = 16

// The max number of unprocessed messages from clients the server can have
// before clients are prevented from sending more messages.
const serverMsgBuffer = 20

// msg is used to pass messages from users around in the server code.
type msg struct {
	// nick is the nickname of the user at the time the message was sent.
	// An empty nickname indicates this is a message from the server.
	nick string
	// text is the message text. It is stored unsanitized.
	text string
	// author points to the client that sent the message. A nil author
	// indicates this message is from the server
	author *client
	// when is when the message was sent.
	when time.Time
	// raw is a string that indicates that is message was pre-rendered to HTML,
	// and doesn't need any processing.
	// TODO: this is a hack to allow the server to queue messages custom
	// messages, like join/leave
	raw string
}

type chatRoom struct {
	// incoming is where messages sent by clients are temporarily stored.
	incoming chan msg
	// quit is used to stop the chatRoom goroutine
	quit chan struct{}
	// limiter rate limits the messages sent to the server for this room.
	// This prevents the server from being spammed by messages.
	limiter *rate.Limiter
	// whenLastMsg is when the most recent message was sent
	whenLastMsg time.Time

	clientsMu sync.Mutex
	clients   map[*client]struct{} // map is used for easy removal
}

func newChatRoom() *chatRoom {
	cr := &chatRoom{
		incoming: make(chan msg, serverMsgBuffer),
		quit:     make(chan struct{}),
		clients:  make(map[*client]struct{}),
		// TODO: is this a good limiter?
		limiter: rate.NewLimiter(rate.Every(time.Millisecond*100), 8),
	}
	go cr.start()
	return cr
}

func (cr *chatRoom) start() {
	for {
		select {
		case <-cr.quit:
			return
		case m := <-cr.incoming:
			cr.limiter.Wait(context.Background())

			authorMsg, chatMsg := cr.handleMsg(m)
			if chatMsg == "" {
				// No message needs to be sent to all clients
				continue
			}
			cr.clientsMu.Lock()
			for c := range cr.clients {
				if m.author == c {
					// This client sent the message, so clear their input field
					c.sendText(authorMsg + clearInputFieldMsg)
				} else {
					c.sendText(chatMsg)
				}
			}
			cr.clientsMu.Unlock()
		}
	}
}

// addClient adds a client to the chat room.
// It also generates a nickname for them.
// The chatServer addClient method should be used by clients instead.
func (cr *chatRoom) addClient(c *client) {
	cr.clientsMu.Lock()
	defer cr.clientsMu.Unlock()

	c.nick = cr.getNewNick()
	cr.clients[c] = struct{}{}
	cr.incoming <- createJoinMsg(c, cr.nicks())
}

// removeClient removes a client from the chat room.
// The chatServer removeClient method should be used by clients instead.
func (cr *chatRoom) removeClient(c *client) {
	cr.clientsMu.Lock()
	defer cr.clientsMu.Unlock()

	delete(cr.clients, c)
	if len(cr.clients) > 0 {
		// Send leave message to clients left in the room
		cr.incoming <- createLeaveMsg(c, cr.nicks())
	}
}

// numClients returns the number of clients in the room.
// It holds the client mutex.
func (cr *chatRoom) numClients() int {
	cr.clientsMu.Lock()
	defer cr.clientsMu.Unlock()
	return len(cr.clients)
}

// nickInUse returns a bool that indicates whether the provided nickname is
// already used by another client. It does not lock the clientsMu, callers
// should do that.
func (cr *chatRoom) nickInUse(nick string) bool {
	for c := range cr.clients {
		if nick == c.nick {
			return true
		}
	}
	return false
}

// getNewNick returns a random nickname that no other client in the room has used.
// It does not lock the clientsMu, callers should do that.
func (cr *chatRoom) getNewNick() string {
	// If nick is already used, just increment number until it's not

	ogNick := genNick()
	nick := ogNick
	i := 2
	for cr.nickInUse(nick) {
		nick = fmt.Sprintf("%s%d", ogNick, i)
		i++
	}
	return nick
}

// nicks returns all the nicknames currently in use in this chat room.
// The nicknames are sorted alphabetically.
// It does not lock the clientsMu, callers should do that.
func (cr *chatRoom) nicks() []string {
	nks := make([]string, len(cr.clients))

	i := 0
	for c := range cr.clients {
		nks[i] = c.nick
		i++
	}
	sort.Strings(nks)
	return nks
}

type client struct {
	// nick (nickname) is the name that appears beside every chat message they send.
	// It is stored sanitized.
	nick string
	// outgoing is where messages to be sent to the client are temporarily stored.
	// It recieves pre-rendered messages, no processing is needed.
	outgoing chan string
	// closeSlow is called if the client can't keep up with messages
	closeSlow func()
}

// sendText tries to send the provided string to the client. If the client's
// outgoing channel is full, the client's closeSlow func is called in a goroutine.
func (c *client) sendText(s string) {
	select {
	case c.outgoing <- s:
	default:
		go c.closeSlow()

	}
}

// chatServer manages all the chat rooms.
// There should only be one instance of it for the site.
type chatServer struct {
	// rooms maps IP address strings to chat rooms
	rooms   map[string]*chatRoom
	roomsMu sync.Mutex

	serveMux http.ServeMux
}

func newChatServer() *chatServer {
	cs := &chatServer{
		rooms: make(map[string]*chatRoom),
	}
	cs.serveMux.Handle("/", noCacheHandler(http.StripPrefix("/", http.FileServer(http.Dir("html")))))
	cs.serveMux.HandleFunc("/connect", cs.connectHandler)
	cs.serveMux.HandleFunc("/admin", noCache(cs.adminHandler))
	cs.serveMux.HandleFunc("/admin.html", noCache(cs.adminHandler))
	cs.serveMux.HandleFunc("/admin-data", cs.adminDataHandler)
	cs.serveMux.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, versionInfo)
	})
	return cs
}

// noCache disables caching of responses.
func noCache(next http.HandlerFunc) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Add("Cache-Control", "no-store, max-age=0")
		rw.Header().Add("Pragma", "no-cache")
		next(rw, r)
	}
}

// noCacheHandler disables caching of responses for http.Handler.
func noCacheHandler(h http.Handler) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Add("Cache-Control", "no-store, max-age=0")
		rw.Header().Add("Pragma", "no-cache")
		h.ServeHTTP(rw, r)
	}
}

// addClient adds a client to the approriate chat room, creating it if needed.
// The room the client is in is returned. It also generates and sets a nickname
// for the client.
func (cs *chatServer) addClient(ip string, c *client) *chatRoom {
	cs.roomsMu.Lock()
	defer cs.roomsMu.Unlock()

	room, ok := cs.rooms[ip]
	if !ok {
		// Room didn't previously exist, create it
		room = newChatRoom()
		cs.rooms[ip] = room
	}

	// Nickname generation happens inside the room func
	room.addClient(c)

	// Insert room name
	c.outgoing <- fmt.Sprintf(`<h2 id="ip-addr">%s</h2>`, ip)

	return room
}

// removeClient removes a client from the approriate chat room, removing the
// entire chat room if it's empty.
func (cs *chatServer) removeClient(ip string, c *client) {
	cs.roomsMu.Lock()
	defer cs.roomsMu.Unlock()

	room, ok := cs.rooms[ip]
	if !ok {
		// Room doesn't exist, so ignore
		log.Printf("chatServer.removeClient: Tried to remove client from non-existent room %s", ip)
		return
	}
	room.removeClient(c)

	if room.numClients() == 0 {
		delete(cs.rooms, ip)
		room.quit <- struct{}{}
	}
}

func (cs *chatServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	cs.serveMux.ServeHTTP(w, r)
}

// connectHandler accepts the WebSocket connection and sets up the duplex messaging.
func (cs *chatServer) connectHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		log.Printf("subscribeHandler: Websocket accept error: %v", err)
		return
	}
	defer conn.Close(websocket.StatusInternalError, "")

	err = cs.connect(r.Context(), getIPString(r), conn)
	if errors.Is(err, context.Canceled) {
		return
	}
	if websocket.CloseStatus(err) == websocket.StatusNormalClosure ||
		websocket.CloseStatus(err) == websocket.StatusGoingAway {
		return
	}
	if err != nil {
		log.Printf("chatServer.connectHandler: %v", err)
		return
	}
}

// htmxJson decodes a JSON websocket message from the web UI, which uses htmx (htmx.org)
// This is the message sent when the user sends a message.
type htmxJson struct {
	Msg     string                 `json:"message"`
	Headers map[string]interface{} `json:"HEADERS"`
}

// connect creates a client and passes messages to and from it.
// If the context is cancelled or an error occurs, it returns and removes the client.
func (cs *chatServer) connect(ctx context.Context, ip string, conn *websocket.Conn) error {
	cl := &client{
		outgoing: make(chan string, clientMsgBuffer),
		closeSlow: func() {
			conn.Close(websocket.StatusPolicyViolation, "connection too slow to keep up with messages")
		},
	}
	room := cs.addClient(ip, cl)
	defer cs.removeClient(ip, cl)

	// Read websocket messages from user into channel
	// Cancel context when connection is closed
	readCh := make(chan string, serverMsgBuffer)
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		for {
			var webMsg htmxJson
			err := wsjson.Read(ctx, conn, &webMsg)
			if err != nil {
				// Treat any error the same as it being closed
				cancel()
				conn.Close(websocket.StatusPolicyViolation, "unexpected error")
				return
			}
			readCh <- webMsg.Msg
		}
	}()

	for {
		select {
		case text := <-cl.outgoing:
			// Send message to user
			err := writeTimeout(ctx, time.Second*5, conn, text)
			if err != nil {
				return err
			}
		case text := <-readCh:
			// Send message to chat room
			room.incoming <- msg{
				nick:   cl.nick,
				text:   text,
				author: cl,
				when:   time.Now(),
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func getIPString(r *http.Request) string {
	xForwardedFor := r.Header.Get("X-Forwarded-For")
	forwardedIPs := strings.Split(xForwardedFor, ", ")
	if len(forwardedIPs) > 0 {
		// The server is reverse-proxied.

		// Return final value in the list, to guard against spoofing
		// https://stackoverflow.com/a/65270044
		return forwardedIPs[len(forwardedIPs)-1]
	}

	// Otherwise, the server is not being reverse-proxied.
	// This is most likely during debugging.

	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		log.Printf("getIPString: net.SplitHostPort(%s): %v", r.RemoteAddr, err)
		return r.RemoteAddr
	}
	parsed := net.ParseIP(ip)

	if parsed.IsPrivate() || parsed.IsLoopback() {
		// IP is from a local address, from the same machine as the server, or from the LAN
		// This would happen during testing, like if the server is being run on a dev machine
		// Return a fake IP address key, as there would be multiple IP addresses within the LAN
		return "lan"
	}
	return ip
}

func writeTimeout(ctx context.Context, timeout time.Duration, conn *websocket.Conn, text string) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	return conn.Write(ctx, websocket.MessageText, []byte(text))
}
