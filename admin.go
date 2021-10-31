package main

// This file has functions that handle the admin interface over HTTP.

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/dustin/go-humanize"
)

// adminHandler serves the admin.html file, but checks if the provided key is correct first.
func (cs *chatServer) adminHandler(w http.ResponseWriter, r *http.Request) {
	query, err := url.QueryUnescape(r.URL.RawQuery)
	if err != nil {
		// Shouldn't happnen unless there's a browser bug I guess
		query = r.URL.RawQuery
	}
	if query != adminKey {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	adminHtml, err := os.Open("html/admin.html")
	if err != nil {
		log.Printf("chatServer.adminHandler: err opening admin.html: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer adminHtml.Close()
	io.Copy(w, adminHtml)
}

func (cs *chatServer) adminDataHandler(rw http.ResponseWriter, r *http.Request) {
	if !strings.HasSuffix(r.Header.Get("HX-Current-URL"), "?"+url.QueryEscape(adminKey)) {
		// Admin data wasn't requested from inside the admin page
		rw.WriteHeader(http.StatusForbidden)
		return
	}

	cs.roomsMu.Lock()
	defer cs.roomsMu.Unlock()

	// Write HTML data as it's processed, but with a buffer
	w := bufio.NewWriter(rw)
	fmt.Fprintf(w, `<p>%d chat rooms</p><hr />`, len(cs.rooms))
	for ip, room := range cs.rooms {
		fmt.Fprintf(
			w, `<h2>%s</h2><p>%d chatters</p><p>Last message: %s</p>`,
			ip, room.numClients(), humanize.RelTime(room.whenLastMsg, time.Now(), "ago", "from now"),
		)
	}
	w.Flush()
}
