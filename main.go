package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"nhooyr.io/websocket"
)

var (
	_region, _instance string
)

func init() {
	_region, _instance = os.Getenv("FLY_REGION"), os.Getenv("FLY_ALLOC_ID")
	if strings.ContainsRune(_instance, '-') {
		_instance = strings.SplitN(_instance, "-", 2)[0]
	}
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"

	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if maybeReplay(w, r) {
			return
		}
		b, _ := json.MarshalIndent(reqData(r), " ", "")
		fmt.Fprint(w, string(b))
	})

	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		if maybeReplay(w, r) {
			return
		}
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{})
		if err != nil {
			log.Printf("%v", err)
			return
		}
		defer c.Close(websocket.StatusNormalClosure, "")
		log.Println("new ws")

		hello, _ := json.Marshal(reqData(r))
		c.Write(r.Context(), websocket.MessageText, []byte(hello))
		for {
			err = echo(r.Context(), c)
			if websocket.CloseStatus(err) == websocket.StatusNormalClosure {
				return
			}
			if err != nil {
				log.Printf("failed to echo with %v: %v", r.RemoteAddr, err)
				return
			}
		}
	})

	log.Println("listening on", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func reqData(r *http.Request) map[string]string {
	return map[string]string{
		"region":   _region,
		"instance": _instance,
		"req_id":   r.Header.Get("Fly-Request-Id"),
		"dispatch": r.Header.Get("Fly-Dispatch-Start"),
	}
}

func maybeReplay(w http.ResponseWriter, r *http.Request) bool {
	var replay string
	if ri := r.URL.Query().Get("replay_instance"); ri != "" && ri != _instance {
		replay = fmt.Sprintf("instance=%s", ri)
	} else if rr := r.URL.Query().Get("replay_region"); rr != "" && rr != _region {
		replay = fmt.Sprintf("region=%s", rr)
	}

	if replay != "" {
		log.Printf("replaying: %q", replay)
		w.Header().Set("fly-replay", replay)
		code := http.StatusConflict
		http.Error(w, http.StatusText(code), code)
		return true
	}
	return false
}

func echo(ctx context.Context, c *websocket.Conn) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	typ, r, err := c.Reader(ctx)
	if err != nil {
		return err
	}

	w, err := c.Writer(ctx, typ)
	if err != nil {
		return err
	}

	_, err = io.Copy(w, r)
	if err != nil {
		return fmt.Errorf("failed to io.Copy: %w", err)
	}

	err = w.Close()
	return err
}
