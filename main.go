/* Copyright 2022 Adam Pritchard. Licensed under Apache License 2.0. */

package main

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"os"
	"regexp"
	"strings"
	"syscall"
	"time"
)

type header struct {
	val   string
	sleep time.Duration
}

type testParams struct {
	host string
	// For automatic Content-Length header, exclude that header
	headers          []header
	body             string
	perByteBodySleep time.Duration

	// This doesn't work yet! There seems to be some read buffering happening internally
	// and our one-byte-at-a-time slow reading isn't working.
	perByteResponseReadSleep time.Duration
}

type conn struct {
	c   net.Conn
	sc  syscall.Conn
	tcp *net.TCPConn
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: httptimeout <config-file.txt>")
		return
	}

	params, err := readConfig(os.Args[1])
	if err != nil {
		panic(fmt.Sprintf("config read failed: %v", err))
	}

	var conn conn

	// Attempt TLS and then fall back to unencrypted
	c, tlsErr := tls.Dial("tcp", params.host, &tls.Config{})
	if tlsErr == nil {
		conn.c = c
		conn.sc = c.NetConn().(syscall.Conn)
		conn.tcp = c.NetConn().(*net.TCPConn)
		fmt.Println("TLS connection to", params.host)
	} else if strings.Contains(tlsErr.Error(), "does not look like a TLS handshake") {
		c, err := net.DialTimeout("tcp", params.host, 3*time.Second)
		if err != nil {
			panic(fmt.Sprintf("net.DialTimeout failed: %v", err))
		}
		conn.c = c
		conn.sc = c.(syscall.Conn)
		conn.tcp = c.(*net.TCPConn)
		fmt.Println("non-TLS connection to", params.host)
	} else {
		panic(fmt.Sprintf("tls.Dial failed: %v", tlsErr))
	}
	fmt.Println()

	conn.tcp.SetNoDelay(true)
	conn.tcp.SetReadBuffer(1)
	conn.tcp.SetWriteBuffer(1)

	// Note that we could test the idle timeout by not closing the connection and sending keep-alives, but then
	defer conn.c.Close()

	startTime := time.Now()

	gotContentLength := false
	for _, h := range params.headers {
		if h.sleep != 0 {
			if err != nil {
				fmt.Println("skipping sleep:", h.sleep)
				continue
			}

			fmt.Println(yellow("sleeping"), h.sleep)
			if slept := sleepWatchConn(h.sleep, conn); slept < h.sleep {
				fmt.Println(red("interrupted after"), slept)
				err = fmt.Errorf("headers sleep interrupted")
			}
		} else {
			if strings.HasPrefix(strings.ToLower(h.val), "content-length:") {
				gotContentLength = true
			}
			err = write(err, conn.c, h.val+"\r\n")
		}
	}
	if !gotContentLength {
		line := fmt.Sprintf("Content-Length: %d", len(params.body))
		err = write(err, conn.c, line+"\r\n")

	}
	err = write(err, conn.c, "\r\n")

	headerTime := time.Now()
	fmt.Printf(cyan("time to send headers: %v\n\n"), headerTime.Sub(startTime))

	if err == nil {
		if !slowWrite(conn, params.perByteBodySleep, []byte(params.body)) {
			fmt.Println(red("\nbody write interrupted"))
		}
	} else {
		fmt.Println("skipping body write")
	}

	bodyTime := time.Now()
	fmt.Printf(cyan("time to send body: %v\n\n"), bodyTime.Sub(headerTime))

	// Attempt to read the response no matter if the writing was interrupted
	ok, lastReadTime := slowRead(conn, params.perByteResponseReadSleep)
	if !ok {
		fmt.Println(red("response read interrupted"))
	}

	fmt.Printf(cyan("time to read response bytes: %v\n"), lastReadTime.Sub(bodyTime))
	fmt.Printf(cyan("time from last read until close/error (~idle timeout): %v\n"), time.Since(lastReadTime))
}

func red(s string) string {
	return fmt.Sprintf("\033[91m%s\033[0m", s)
}

func yellow(s string) string {
	return fmt.Sprintf("\033[93m%s\033[0m", s)
}

func cyan(s string) string {
	return fmt.Sprintf("\033[96m%s\033[0m", s)
}

func sleepWatchConn(sleep time.Duration, conn conn) time.Duration {
	increment := 100 * time.Millisecond

	start := time.Now()
	for time.Since(start) < sleep {
		time.Sleep(increment)
		if err := connCheck(conn.sc); err != nil {
			break
		}
	}
	return time.Since(start)
}

func slowWrite(conn conn, perByteSleep time.Duration, b []byte) bool {
	for i := 0; i < len(b); i++ {
		if i != 0 {
			// If we try to use sleepWatchConn here it won't have the desired effect.
			// sleepWatchConn checks if the read side of the connection is open, but we're
			// writing. We might be able to write even if reading is broken and might not
			// be able to write even if read is working.
			time.Sleep(perByteSleep)
		}

		fmt.Print(string(b[i]))
		n, err := conn.c.Write(b[i : i+1])
		if err != nil || n != 1 {
			return false
		}
	}
	fmt.Println()
	return true
}

func slowRead(conn conn, perByteSleep time.Duration) (bool, time.Time) {
	// Read HTTP readers will look at Content-Length or chunk size and know when they're
	// done reading. But this is a dumb byte reader that will keep trying to read until
	// the idle timeout forcibly kicks it off.

	incoming := make(chan byte)
	readErr := make(chan error)
	go func() {
		buf := make([]byte, 1)
		first := true
		for {
			if !first {
				sleepWatchConn(perByteSleep, conn)
			}
			first = false

			_, err := conn.c.Read(buf)
			if err != nil {
				readErr <- err
				return
			}

			incoming <- buf[0]
		}
	}()

	var lastByteTime time.Time
	var needNewline bool
outer:
	for {
		select {
		case err := <-readErr:
			if err == io.EOF {
				break outer
			}
			if needNewline {
				fmt.Println()
			}
			fmt.Println("read error:", err)
			return false, time.Time{}
		case b := <-incoming:
			fmt.Print(string(b))
			lastByteTime = time.Now()
			needNewline = true
		case <-time.After(10 * time.Second):
			if needNewline {
				fmt.Println()
			}
			needNewline = false
			fmt.Println(yellow("10 seconds with no bytes read (waiting for idle timeout?)"))
		}
	}
	fmt.Println()

	return true, lastByteTime
}

func write(currErr error, w io.Writer, s string) error {
	if currErr != nil {
		fmt.Printf("skipping %q\n", s)
		return currErr
	}

	fmt.Print(s)
	n, err := w.Write([]byte(s))
	if err != nil {
		fmt.Println(err)
		return err
	}
	if n != len(s) {
		err = fmt.Errorf("wrote wrong length: %d vs %d", n, len(s))
		fmt.Println(err)
		return err
	}

	return nil
}

func readConfig(filename string) (testParams, error) {
	// Open the file for reading
	f, err := os.Open(filename)
	if err != nil {
		return testParams{}, fmt.Errorf("failed to open config file %q: %w", filename, err)
	}
	defer f.Close()

	sleepRegexp := regexp.MustCompile(`^sleep (\S+)`)
	perByteBodySleepRegexp := regexp.MustCompile(`^PerByteBodySleep:\s*(\S+)`)
	perByteResponseReadSleepRegexp := regexp.MustCompile(`^PerByteResponseReadSleep:\s*(\S+)`)

	var res testParams
	phase := "host"

	reader := bufio.NewReader(f)
	for {
		// Read the next line
		line, isPrefix, err := reader.ReadLine()
		if err == io.EOF {
			break
		} else if err != nil {
			return testParams{}, err
		} else if isPrefix {
			// TODO: read full
			return testParams{}, fmt.Errorf("config line too long")
		}

		lineStr := string(line)

		if lineStr == "" {
			switch phase {
			case "host":
				phase = "headers"
			case "headers":
				phase = "byte-sleeps"
			case "byte-sleeps":
				phase = "body"
			}
			continue
		}

		if strings.HasPrefix(lineStr, "#") {
			// comment
			continue
		}

		switch phase {
		case "host":
			res.host = lineStr
		case "headers":
			if match := sleepRegexp.FindStringSubmatch(lineStr); match != nil {
				sleep, err := time.ParseDuration(match[1])
				if err != nil {
					return testParams{}, fmt.Errorf("got bad header sleep in config: %q; %w", lineStr, err)
				}
				res.headers = append(res.headers, header{sleep: sleep})
			} else {
				res.headers = append(res.headers, header{val: lineStr})
			}
		case "byte-sleeps":
			if match := perByteBodySleepRegexp.FindStringSubmatch(lineStr); match != nil {
				sleep, err := time.ParseDuration(match[1])
				if err != nil {
					return testParams{}, fmt.Errorf("got bad PerByteBodySleep in config: %q; %w", lineStr, err)
				}
				res.perByteBodySleep = sleep
			} else if match := perByteResponseReadSleepRegexp.FindStringSubmatch(lineStr); match != nil {
				sleep, err := time.ParseDuration(match[1])
				if err != nil {
					return testParams{}, fmt.Errorf("got bad PerByteResponseReadSleep in config: %q; %w", lineStr, err)
				}
				res.perByteResponseReadSleep = sleep
			} else {
				return testParams{}, fmt.Errorf("got unexpected byte-sleep: %q", lineStr)
			}

		case "body":
			if res.body != "" {
				res.body += "\n"
			}
			res.body += lineStr
		}
	}

	return res, nil
}
