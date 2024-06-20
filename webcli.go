package webcli

import (
	"context"
	"embed"
	"fmt"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/igolaizola/webcli/pkg/config"
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
	Array       bool
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

type Option func(*options) error

// WithAppName sets the application name.
// The application name is used in the title of the web page.
func WithAppName(name string) Option {
	return func(o *options) error {
		o.app = name
		return nil
	}
}

// WithAddress sets the address where the server will listen.
// The address should be in the format "host:port".
func WithAddress(addr string) Option {
	return func(o *options) error {
		if addr == "" {
			return fmt.Errorf("webcli: address can't be empty")
		}
		o.address = addr
		return nil
	}
}

// WithConfigPath sets the function to generate the path of the config file.
// The function receives the command name and should return the path of the
// config file.
// By default, the config file is located in the "cfg" folder with the name of
// the command in YAML format.
func WithConfigPath(fn func(cmdName string) string) Option {
	return func(o *options) error {
		if fn == nil {
			return fmt.Errorf("webcli: config path function can't be nil")
		}
		o.configPath = fn
		return nil
	}
}

// DefaultYAMLConfigPath returns the default path for the config file in YAML
// format.
var DefaultYAMLConfigPath = func(cmdName string) string {
	cmdName = strings.ReplaceAll(cmdName, "/", ".")
	return fmt.Sprintf("cfg/%s.yaml", cmdName)
}

// DefaultJSONConfigPath returns the default path for the config file in JSON
// format.
var DefaultJSONConfigPath = func(cmdName string) string {
	cmdName = strings.ReplaceAll(cmdName, "/", ".")
	return fmt.Sprintf("cfg/%s.json", cmdName)
}

// WithReadConfig sets the function to read the config file.
func WithReadConfig(fn func(path string) (map[string]any, error)) Option {
	return func(o *options) error {
		if fn == nil {
			o.disableConfig = true
		}
		o.readConfig = fn
		return nil
	}
}

// WithWriteConfig sets the function to write the config file.
func WithWriteConfig(fn func(path string, values map[string]any) error) Option {
	return func(o *options) error {
		if fn == nil {
			return fmt.Errorf("webcli: write config function can't be nil")
		}
		o.writeConfig = fn
		return nil
	}
}

// WithDisableConfig disables the config file.
func WithDisableConfig() Option {
	return func(o *options) error {
		o.disableConfig = true
		return nil
	}
}

type options struct {
	app     string
	address string

	disableConfig bool
	configPath    func(cmdName string) string
	readConfig    func(path string) (map[string]any, error)
	writeConfig   func(path string, values map[string]any) error
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
func New(commands []*Command, opts ...Option) (*Server, error) {
	// Default options
	o := &options{
		app:        "WebCLI",
		address:    ":0",
		configPath: DefaultYAMLConfigPath,
		readConfig: func(path string) (map[string]any, error) {
			return config.Read(path)
		},
		writeConfig: func(path string, values map[string]any) error {
			return config.Write(path, values)
		},
	}

	// Override options
	for _, opt := range opts {
		if err := opt(o); err != nil {
			return nil, err
		}
	}

	// Create a context for handling the server
	ctx, cancel := context.WithCancel(context.Background())

	// Convert command tree to a flat list
	parsedCmds := parseCommands(commands, "")

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
		v := view.List(o.app, cmds)
		if err := v.Render(r.Context(), w); err != nil {
			log.Println("webcli: couldn't render view:", err)
		}
	}))

	// Command page handler
	for name, cmd := range cmdLookup {
		path := fmt.Sprintf("/commands/%s", name)
		mux.Handle(path, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("HX-Push-Url", path)

			// Check if the form should use default values
			useDefault := len(r.URL.Query()["default"]) > 0

			// Read values from the config file
			configValues := map[string]string{}
			if !o.disableConfig && !useDefault {
				candidate, err := o.readConfig(o.configPath(name))
				if err != nil {
					log.Println("webcli:", err)
				} else {
					for k, v := range candidate {
						configValues[k] = fmt.Sprintf("%v", v)
					}
				}
			}

			// Create form fields
			var fields []view.Field
			for _, f := range cmd.Fields {
				t := view.Text
				switch f.Type {
				case Number:
					t = view.Number
				case Boolean:
					t = view.Boolean
				}
				def := f.Default

				// Check if the value is in the config file
				if v, ok := configValues[f.Name]; ok {
					def = v
				}

				vf := view.Field{
					Name:        f.Name,
					Default:     def,
					Description: f.Description,
					Type:        t,
					Array:       f.Array,
				}
				fields = append(fields, vf)
			}

			// Render the form
			v := view.Form(o.app, name, fields, !o.disableConfig)
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
		v := view.ListLog(o.app, logs)
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
		v := view.Log(o.app, id, proc.Logs())
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

	// Command save handler
	if !o.disableConfig {
		mux.Handle("/close", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		mux.Handle("/save", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

			// Get command name
			cmdName := r.FormValue("command")
			if cmdName == "" {
				httpError(w, "command field is empty", http.StatusBadRequest)
				return
			}
			cmd, ok := cmdLookup[cmdName]
			if !ok {
				httpError(w, "command not found", http.StatusNotFound)
				return
			}

			// Get fields from the form
			var fields = map[string][]string{}
			for k, vs := range r.Form {
				if k == "command" {
					continue
				}
				if len(vs) == 0 {
					continue
				}
				for _, v := range vs {
					if _, ok := fields[k]; !ok {
						fields[k] = []string{}
					}
					fields[k] = append(fields[k], v)
				}
			}

			// Convert values to a map of single values if not an array
			var values = map[string]any{}
			for _, f := range cmd.Fields {
				if vs, ok := fields[f.Name]; ok {
					switch {
					case f.Array:
						if len(vs) == 1 && vs[0] == "" {
							values[f.Name] = []string{}
						} else {
							values[f.Name] = vs
						}
					case f.Type == Boolean:
						values[f.Name] = vs[0] == "on"
					case f.Type == Number:
						if num, err := strconv.Atoi(vs[0]); err == nil {
							values[f.Name] = num
						} else if num, err := strconv.ParseFloat(vs[0], 64); err == nil {
							values[f.Name] = num
						} else {
							values[f.Name] = 0
						}
					default:
						values[f.Name] = vs[0]
					}
				}
			}

			// Write values to the config file
			if err := o.writeConfig(o.configPath(cmdName), values); err != nil {
				log.Println("webcli:", err)
				v := view.SaveError()
				if err := v.Render(r.Context(), w); err != nil {
					log.Println("webcli: couldn't render view:", err)
				}
				return
			}

			// Save modal
			v := view.Save()
			if err := v.Render(r.Context(), w); err != nil {
				log.Println("webcli: couldn't render view:", err)
			}
		}))
	}

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
		for k, vs := range r.Form {
			if k == "command" {
				continue
			}
			for _, value := range vs {
				// Convert checkbox on/off to true/false
				switch value {
				case "on":
					value = "true"
				case "off":
					value = "false"
				}
				args = append(args, fmt.Sprintf("--%s=%s", k, value))
			}
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
		v := view.Log(o.app, id, proc.logs)
		if err := v.Render(r.Context(), w); err != nil {
			log.Println("webcli: couldn't render view:", err)
		}
	}))

	return &Server{
		Handler:    mux,
		cancel:     cancel,
		customAddr: o.address,
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
