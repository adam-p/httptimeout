# HTTP timeout test client

While testing Go HTTP server timeouts I wrote this little tool to help me test. It allows for slowing down header write and body write. (I want it to also slow down response read, but I haven't quite figured out how yet.)

In particular, this was intended to test the behaviour of Go's http.Server ReadTimeout, ReadHeaderTimeout, and TimeoutHandler.

It uses a [simple config file](config-example.txt) and can talk HTTPS and HTTP.

If you want to learn more about Go's HTTP server timeouts, I [wrote a blog post](https://crypti.cc/blog/2022/01/15/golang-http-server-timeouts.html) about it.

```no-hightlight
$ go run . config-example.txt

non-TLS connection to localhost:8585

POST /login HTTP/1.1
Host: localhost:8585
sleeping 1.5s
User-Agent: httptimeout
X-Requested-With: XMLHttpRequest
Connection: keep-alive
Content-Length: 74

time to send headers: 1.5236624s

{"username":"x","password":
body write interrupted
time to send body: 2.6416289s

HTTP/1.1 503 Service Unavailable
Date: Sat, 15 Jan 2022 23:14:16 GMT
Content-Length: 0
Connection: close


time to read response bytes: 39.4457ms
time from last read until close/error (~idle timeout): 2.1301ms
```

It works better on non-Windows systems, as it can detect a broken write connection and more accurately report when it happened. It also unconditionally uses console colours, so won't look right in some terminals. Use WSL on Windows.

If you want to turn this into a one-file script(ish), put the function in conncheck_posix.go into main.go.

Note that Go 1.18 is required, to use tls.Conn.NetConn.

I tried to implement a slow read as well, but never got it working against my server. It seemed like the response was always being buffered somewhere, so the http.Server.WriteTimeout never triggered. If anyone knows how I can force that, I'm happy to hear.

Much of what this does can be accomplished with netcat, careful typing or pasting, and a stopwatch, but that's a hassle.
