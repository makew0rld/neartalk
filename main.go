package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"time"
)

// Flag vars
var (
	host        string
	port        uint
	adminKey    string
	versionFlag bool
)

func main() {
	flag.StringVar(&host, "host", "127.0.0.1", "Host for HTTP server")
	flag.UintVar(&port, "port", 8000, "Port number for HTTP server")
	flag.StringVar(&adminKey, "key", "", "Key/password to access admin interface")
	flag.BoolVar(&versionFlag, "version", false, "See version info")
	flag.Parse()

	if versionFlag {
		fmt.Print(versionInfo)
		return
	}
	if adminKey == "" {
		fmt.Println("No admin key set! Use -help for details.")
		return
	}

	err := run()
	if err != nil {
		log.Fatal(err)
	}
}

func run() error {
	rand.Seed(time.Now().UnixNano())

	l, err := net.Listen("tcp", net.JoinHostPort(host, strconv.FormatUint(uint64(port), 10)))
	if err != nil {
		return err
	}
	log.Printf("listening on http://%v", l.Addr())

	// Create and run HTTP server
	cs := newChatServer()
	s := &http.Server{
		Handler:      cs,
		ReadTimeout:  time.Second * 10,
		WriteTimeout: time.Second * 10,
	}
	errc := make(chan error, 1)
	go func() {
		errc <- s.Serve(l)
	}()

	// Wait for server error or process signals (like Ctrl-C)
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt)
	select {
	case err := <-errc:
		log.Printf("failed to serve: %v", err)
	case sig := <-sigs:
		log.Printf("terminating: %v", sig)
	}

	// Gracefully shut down HTTP server with 5 second timeout
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()
	return s.Shutdown(ctx)
}
