package main

import (
	"embed"
	"flag"
	"html/template"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"unsafe"

	"github.com/creack/pty"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
)

//go:embed assets/*
var StaticFiles embed.FS

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func handleWebsocketTerminal(c *gin.Context) {
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	l := log.WithField("remoteaddr", "local")
	if err != nil {
		l.WithError(err).Error("Unable to upgrade connection")
		return
	}

	cmd := exec.Command("/bin/bash", "-l")
	cmd.Env = append(os.Environ(), "TERM=xterm")

	tty, err := pty.Start(cmd)
	if err != nil {
		l.WithError(err).Error("Unable to start pty/cmd")
		conn.WriteMessage(websocket.TextMessage, []byte(err.Error()))
		return
	}
	defer func() {
		cmd.Process.Kill()
		cmd.Process.Wait()
		tty.Close()
		conn.Close()
	}()
	window := struct {
        row uint16
        col uint16
        x   uint16
        y   uint16
    }{
        uint16(1000),
        uint16(500),
        0,
        0,
    }	
                      _, _, errno := syscall.Syscall(
                              syscall.SYS_IOCTL,
                              tty.Fd(),
                              syscall.TIOCSWINSZ,
                              uintptr(unsafe.Pointer(&window)))
	if errno !=0 {
	l.WithError(syscall.Errno(errno))
	}
	go func() {
		for {
			buf := make([]byte, 1024)

			read, err := tty.Read(buf)

			if err != nil {
				conn.WriteMessage(websocket.TextMessage, []byte(err.Error()))
				l.WithError(err).Error("Unable to read from pty/cmd")
				return
			}
			conn.WriteMessage(websocket.TextMessage, buf[:read])
		}
	}()

	for {
		messageType, reader, err := conn.NextReader()
		if err != nil {
			l.WithError(err).Error("Unable to grab next reader")
			return
		}

		if messageType == websocket.TextMessage {
			dataTypeBuf := make([]byte, 1)
			_, err := reader.Read(dataTypeBuf)
			if err != nil {
				l.WithError(err).Error("Unable to read message type from reader")
				conn.WriteMessage(websocket.TextMessage, []byte("Unable to read message type from reader"))
				return
			}
			// copied, err := io.Copy(tty, reader)
			copied, err := io.WriteString(tty, string(dataTypeBuf))

			if err != nil {
				l.WithError(err).Errorf("Error after copying %d bytes", copied)
			}

		}
	}
}

func main() {
	var listen = flag.String("listen", "127.0.0.1:3000", "Host:port to listen on")

	flag.Parse()

	r := gin.Default()
	authorized := r.Group("/", gin.BasicAuth(gin.Accounts{
        "secret": "R4vrbuWnr7RBub38",}))

	t, _ := template.ParseFS(StaticFiles, "assets/index.html")
	r.SetHTMLTemplate(t)
	r.GET("/term", handleWebsocketTerminal)
	r.StaticFS("/static", http.FS(StaticFiles))

	authorized.GET("/", func(c *gin.Context) {
		c.HTML(200, "index.html", "")
	})

	if !(strings.HasPrefix(*listen, "127.0.0.1") || strings.HasPrefix(*listen, "localhost")) {
		log.Warn("Danger Will Robinson - This program has no security built in and should not be exposed beyond localhost, you've been warned")
	}

	r.Run(*listen)
}
