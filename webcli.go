package webcli

import (
	"context"
	"embed"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/igolaizola/webcli/pkg/view"
)

type Command struct {
	Fields      []*Field
	Name        string
	Description string
	Subcommands []*Command
}

type Field struct {
	Name        string
	Default     string
	Description string
	Type        FieldType
}

type FieldType int

const (
	Text FieldType = iota
	Number
	Boolean
)

type parsedCommand struct {
	Fields      []*Field
	Name        string
	Description string
}

type Config struct {
	App      string
	Commands []*Command
	Address  string
}

type Server struct {
	Handler http.Handler
	Port    int

	customAddr string
	cancel     context.CancelFunc
	httpServer *http.Server
}

//go:embed static/*
var staticContent embed.FS

// Server serves the webcli server.
func New(cfg *Config) (*Server, error) {
	ctx, cancel := context.WithCancel(context.Background())

	app := "WebCLI"
	if cfg.App != "" {
		app = cfg.App
	}
	customAddress := ":0"
	if cfg.Address != "" {
		customAddress = cfg.Address
	}

	// Convert command tree to a flat list
	parsedCmds := parseCommands(cfg.Commands, "")

	// Create lookup and name list
	var cmdLookup = map[string]*parsedCommand{}
	var cmdNames []string
	for _, cmd := range parsedCmds {
		cmdLookup[cmd.Name] = cmd
		cmdNames = append(cmdNames, cmd.Name)
	}

	// Multiplexer for the server
	mux := http.NewServeMux()

	// Static files handler
	mux.Handle("/static/", http.FileServer(http.FS(staticContent)))

	// Index page handler
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("HX-Push-Url", "/")
		var cmds []view.CommandEntry
		for _, name := range cmdNames {
			cmds = append(cmds, view.CommandEntry{
				Name:        name,
				Description: cmdLookup[name].Description,
			})
		}
		v := view.List(app, cmds)
		if err := v.Render(r.Context(), w); err != nil {
			log.Println("webcli: couldn't render view:", err)
		}
	}))

	// Command page handler
	for name, cmd := range cmdLookup {
		path := fmt.Sprintf("/commands/%s", name)
		mux.Handle(path, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("HX-Push-Url", path)
			var fields []view.Field
			for _, f := range cmd.Fields {
				t := view.Text
				switch f.Type {
				case Number:
					t = view.Number
				case Boolean:
					t = view.Boolean
				}
				vf := view.Field{
					Name:        f.Name,
					Default:     f.Default,
					Description: f.Description,
					Type:        t,
				}

				fields = append(fields, vf)
			}
			v := view.Form(app, name, fields)
			if err := v.Render(r.Context(), w); err != nil {
				log.Println("webcli: couldn't render view:", err)
			}
		}))
	}

	var lck sync.Mutex
	processes := map[string]*process{}

	// Event stream handler
	mux.Handle("/events/{id}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Get process ID
		id := r.PathValue("id")
		if id == "" {
			httpError(w, "id is empty", http.StatusBadRequest)
			return
		}
		lck.Lock()
		proc, ok := processes[id]
		lck.Unlock()

		// If the process doesn't exist, return a close event
		if !ok {
			fmt.Fprint(w, "event: close\ndata: <div></div>\n\n")
			return
		}

		// Set headers for SSE
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		// Create a channel to send data
		dataC := make(chan string)
		defer close(dataC)

		// Create a context for handling client disconnection
		_, cancel := context.WithCancel(r.Context())
		defer cancel()

		// Generate a random ID
		subID := fmt.Sprintf("%d", time.Now().UnixNano())

		// Subscribe to the process logs
		proc.Subscribe(subID, func(text string, close bool) {
			text = strings.ReplaceAll(text, "\n", "<br>")
			text = fmt.Sprintf("event: log\ndata: %s\n\n", text)
			dataC <- text
			if close {
				text = "event: close\ndata: <div></div>\n\n"
				dataC <- text
				cancel()
			}
		})
		defer proc.Unsubscribe(subID)

		// Send event data to the client
		for {
			select {
			case <-ctx.Done():
				return
			case <-r.Context().Done():
				return
			case data := <-dataC:
				fmt.Fprint(w, data)
				w.(http.Flusher).Flush()
			}
		}
	}))

	// Log list page handler
	mux.Handle("/logs", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("HX-Push-Url", "/logs")
		var logs []view.LogEntry
		for id, p := range processes {
			logs = append(logs, view.LogEntry{
				ID:       id,
				Command:  p.command,
				Start:    p.start,
				End:      p.end,
				Error:    p.error,
				Canceled: p.canceled,
			})
		}
		v := view.ListLog(app, logs)
		if err := v.Render(r.Context(), w); err != nil {
			log.Println("webcli: couldn't render view:", err)
		}
	}))

	// Log page handler
	logHandler := func(w http.ResponseWriter, r *http.Request, cancel bool) {
		// Get process ID
		id := r.PathValue("id")
		proc, ok := processes[id]
		if !ok {
			httpError(w, "reader not found", http.StatusNotFound)
			return
		}
		if cancel {
			proc.cancel()
		}
		w.Header().Set("HX-Push-Url", fmt.Sprintf("/logs/%s", id))
		v := view.Log(app, id, proc.Logs())
		if err := v.Render(r.Context(), w); err != nil {
			log.Println("webcli: couldn't render view:", err)
		}
	}
	mux.Handle("/logs/{id}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logHandler(w, r, false)
	}))

	// Cancel command handler
	mux.Handle("/cancel/{id}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logHandler(w, r, true)
	}))

	// Command run handler
	mux.Handle("/run", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only post method is allowed
		if r.Method != http.MethodPost {
			httpError(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Parse form
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Execute command
		cmdName := r.FormValue("command")
		if cmdName == "" {
			httpError(w, "command field is empty", http.StatusBadRequest)
			return
		}
		args := []string{cmdName}
		for k, v := range r.Form {
			if k == "command" {
				continue
			}
			// Convert checkbox on/off to true/false
			value := v[0]
			switch value {
			case "on":
				value = "true"
			case "off":
				value = "false"
			}
			args = append(args, fmt.Sprintf("--%s=%s", k, value))
		}
		id := strings.Replace(time.Now().Format("20060102-150405.999"), ".", "-", 1)
		proc, err := newProcess(ctx, args)
		if err != nil {
			httpError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		lck.Lock()
		processes[id] = proc
		lck.Unlock()

		// Replace URL
		w.Header().Set("HX-Push-Url", fmt.Sprintf("/logs/%s", id))
		// Log page
		v := view.Log(app, id, proc.logs)
		if err := v.Render(r.Context(), w); err != nil {
			log.Println("webcli: couldn't render view:", err)
		}
	}))

	return &Server{
		Handler:    mux,
		cancel:     cancel,
		customAddr: customAddress,
	}, nil
}

// Run starts the webcli server and blocks until the context is done.
func (s *Server) Run(ctx context.Context) error {
	// Start the server
	if err := s.Start(ctx); err != nil {
		return err
	}
	u := fmt.Sprintf("http://localhost:%d", s.Port)
	if s.customAddr != ":0" {
		u = s.customAddr
	}
	fmt.Println("Open in your browser", u)

	// Wait for the context to be done
	<-ctx.Done()

	// Stop the server
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.Stop(ctx)
}

// Serve starts the webcli server.
func (s *Server) Start(ctx context.Context) error {
	// Create an HTTP server with the custom ServeMux
	httpServer := &http.Server{
		Handler: s.Handler,
	}

	// Create a listener on a random port or a custom address
	addr := strings.TrimPrefix(s.customAddr, "https://")
	addr = strings.TrimPrefix(addr, "http://")
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("webcli: failed to listen on a %s: %w", addr, err)
	}

	// Extract the port from the listener address
	s.Port = listener.Addr().(*net.TCPAddr).Port
	s.httpServer = httpServer

	// Start the server in a goroutine so that it doesn't block
	go func() {
		if err := httpServer.Serve(listener); err != http.ErrServerClosed {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	return nil
}

// Stop stops the webcli server.
func (s *Server) Stop(ctx context.Context) error {
	defer s.cancel()
	if s.httpServer != nil {
		if err := s.httpServer.Shutdown(ctx); err != nil {
			return fmt.Errorf("webcli: server shutdown failed: %v", err)
		}
	}
	return nil
}

func parseCommands(cmds []*Command, parent string) []*parsedCommand {
	var all []*parsedCommand
	for _, cmd := range cmds {
		p := parseCommand(cmd, parent)
		if p != nil {
			all = append(all, p)
		}
		if cmd.Subcommands != nil {
			all = append(all, parseCommands(cmd.Subcommands, cmd.Name)...)
		}
	}
	return all
}

func parseCommand(cmd *Command, parent string) *parsedCommand {
	name := cmd.Name
	if parent != "" {
		name = fmt.Sprintf("%s/%s", parent, name)
	}
	parsed := &parsedCommand{
		Name:        name,
		Description: cmd.Description,
		Fields:      cmd.Fields,
	}
	if len(cmd.Fields) == 0 {
		// If it doesn't have flags, it's just a holder of subcommands
		if len(cmd.Subcommands) > 0 {
			return nil
		}
		return parsed
	}
	return parsed
}

func httpError(w http.ResponseWriter, msg string, code int) {
	log.Println(msg)
	http.Error(w, msg, code)
}
