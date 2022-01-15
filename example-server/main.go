/* Copyright 2022 Adam Pritchard. Licensed under Apache License 2.0. */

package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"
)

func main() {
	makeHandler := func(handlerTimeout time.Duration) http.Handler {
		return statusLoggerMiddleware(http.TimeoutHandler(http.HandlerFunc(requestHandler), handlerTimeout, ""))
	}

	srv := &http.Server{
		ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout:       4 * time.Second,
		WriteTimeout:      5 * time.Second,
		IdleTimeout:       13 * time.Second,
		Handler:           makeHandler(3 * time.Second),

		Addr: "localhost:8585",
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		log.Fatal(srv.ListenAndServe())
		wg.Done()
	}()

	fmt.Printf("listening on %s\n", srv.Addr)
	wg.Wait()
}

func requestHandler(w http.ResponseWriter, req *http.Request) {
	startTime := time.Now()

	fmt.Println("\n url:", req.URL.String())
	fmt.Println("hdrs:", req.Header)

	body, err := io.ReadAll(req.Body)
	defer req.Body.Close()

	readTime := time.Now()
	fmt.Println("body read time:", readTime.Sub(startTime))

	if err != nil {
		fmt.Printf("body read error: %v\n", err)
	}
	fmt.Println("body:", string(body))

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("this is the response from the server"))

	fmt.Printf("total time: %v; time since body read:%v\n", time.Since(startTime), time.Since(readTime))
}

func statusLoggerMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		srrw := &statusRecorderResponseWriter{ResponseWriter: w}
		next.ServeHTTP(srrw, req)

		if srrw.Status == 503 {
			fmt.Println("responded with status: 503 REQUEST TIMEOUT", srrw.Status)
		} else {
			fmt.Println("responded with status:", srrw.Status)
		}

	})
}

type statusRecorderResponseWriter struct {
	http.ResponseWriter
	Status int
}

func (r *statusRecorderResponseWriter) WriteHeader(status int) {
	r.Status = status
	r.ResponseWriter.WriteHeader(status)
}

/*
func (r *statusRecorderResponseWriter) Write(b []byte) (int, error) {
	// We're going to flush (chunk) each byte in an attempt to de-buffer the write, but it doesn't seem to work

	var totalN int
	for i := range b {
		n, err := r.ResponseWriter.Write(b[i : i+1])
		totalN += n
		if err != nil {
			return totalN, err
		}

		r.ResponseWriter.(http.Flusher).Flush()
	}
	return totalN, nil
}
*/
