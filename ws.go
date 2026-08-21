package rpeat

import (
	"fmt"
	"github.com/gorilla/websocket"
	"net/http"
	"time"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

type wSClient struct {
	pool           *wSClientPool
	conn           *websocket.Conn
	updates        chan *JobUpdate
	user           string
	authorizedJobs []string
}
type wSClientPool struct {
	clients map[*wSClient]bool
	// need to be able to remove closed connections
	register   chan *wSClient
	unregister chan *wSClient
	updates    chan *JobUpdate
}

func NewWSClientPool() *wSClientPool {
	return &wSClientPool{
		clients:    make(map[*wSClient]bool),
		register:   make(chan *wSClient, 50),
		unregister: make(chan *wSClient, 50),
		updates:    make(chan *JobUpdate, 50),
	}
}

// total client (websocket) pool - responsible for registering and unregistering clients as well as
// replicating any server updates to all connected clients
func (pool *wSClientPool) watch(updates chan *JobUpdate) {
	for {
		select {
		case client := <-pool.register:
			ConnectionLogger.Printf("Websocket: registering new client at %s", client.conn.RemoteAddr())
			pool.clients[client] = true
		case client := <-pool.unregister:
			ConnectionLogger.Printf("Websocket: unregistering client at %s", client.conn.RemoteAddr())
			delete(pool.clients, client)
			client.conn.Close()
		case update := <-updates:
			// TODO: mechanism(s) to send server updates (read only) to ws and remotes
			// - send outbound API updates
			// - send serverUpdate<- update
			for c := range pool.clients {
				if stringInSlice(update.Uuid, c.authorizedJobs) {
					c.updates <- update
				}
			}
		}
	}
}

// Routine to unregister lost clients - notably behind
// firewall or lost connection that are not explicitely browser terminated
func (client *wSClient) heartbeat() {
	pongWait := 5 * time.Second
	defer func() {
		// FIXME: this may lag the client unregister request, and attempt to close a closed client.
		// possible alternatives: https://go101.org/article/channel-closing.html
		client.pool.unregister <- client
	}()
	client.conn.SetReadDeadline(time.Now().Add(pongWait))
	client.conn.SetPongHandler(func(string) error { client.conn.SetReadDeadline(time.Now().Add(pongWait)); return nil })
	for {
		_, _, err := client.conn.ReadMessage()
		if err != nil {
			ConnectionLogger.Printf("lost heartbeat from %s: %v", client.conn.RemoteAddr(), err)
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				ConnectionLogger.Printf("is unexpected close error from %s: %v", client.conn.RemoteAddr(), err)
			}
			break
		}
	}
}

// per client (websocket) watcher for updates or heartbeat (elapsed time counter?) - client.updates<- are broadcast from pool.watch
func (client *wSClient) watch() {
	t := time.NewTimer(time.Millisecond)
	for {
		select {
		case u := <-client.updates:
			if err := client.conn.WriteJSON(u); err != nil {
				ConnectionLogger.Printf("write error (%s): %s", client.conn.RemoteAddr(), err.Error())
				return
			} else if u != nil {
				ConnectionLogger.Printf("sending UPDATE Uuid:%s Modified:%d to %s", u.Uuid, u.Modified, client.conn.RemoteAddr())
			}
		case <-t.C:
			type ServerTime struct {
				Modified int64
				Tzoffset int
				Tzname   string
			}
			now := time.Now()
			tzname, tzoffset := time.Now().Zone()

			timestamp := &ServerTime{Modified: now.Unix(), Tzoffset: tzoffset, Tzname: tzname}
			if err := client.conn.WriteJSON(timestamp); err != nil {
				ConnectionLogger.Printf(ErrorColor, fmt.Sprintf("ping errors: %s", err.Error()))
				ConnectionLogger.Printf(ErrorColor, "sending unregister")
				client.pool.unregister <- client
				ConnectionLogger.Printf(ErrorColor, "sent unregister")
				t.Stop()
				return
			}
			t.Reset(time.Second)
			if err := client.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				ConnectionLogger.Printf("write error (%s): %s", client.conn.RemoteAddr(), err.Error())
				return
			}
		}
	}
}
